package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"

	"golang.org/x/crypto/ssh"

	"github.com/nagiflow/nagipath/internal/keys"
)

var ErrNoCredential = errors.New("no credential resolves for this node")

type Credential struct {
	ID          int64
	Name        string
	Username    string
	AuthKind    string
	PublicKey   string
	Fingerprint string
	ExternalRef string
	CreatedAt   string
}

// PasswordCredentialKinds are profiles whose secret is a write-only password.
var PasswordCredentialKinds = map[string]bool{
	"username_password": true,
	"kerberos":          true,
}

// CreateCredential seals the private key material and stores it. The AAD binds
// each ciphertext to its own row and column, so the two-step insert (row first,
// ciphertext second) is deliberate: the row id is part of the AAD.
func (db *DB) CreateCredential(ctx context.Context, m *keys.Master, name, username, authKind, privateKey, passphrase, certificate string, by *int64) (int64, error) {
	signer, err := parseSigner(privateKey, passphrase)
	if err != nil {
		return 0, fmt.Errorf("private key rejected: %w", err)
	}
	pub := string(ssh.MarshalAuthorizedKey(signer.PublicKey()))
	fp := ssh.FingerprintSHA256(signer.PublicKey())

	tx, err := db.W.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `INSERT INTO credential
		(name, username, auth_kind, private_key_ct, private_key_nonce,
		 public_key, key_fingerprint, created_at, created_by)
		VALUES (?,?,?,x'',x'',?,?,?,?)`,
		name, username, authKind, pub, fp, Now(), by)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}

	ct, nonce, err := m.Seal([]byte(privateKey), keys.AAD("credential", "private_key", id))
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE credential SET private_key_ct = ?, private_key_nonce = ? WHERE id = ?`,
		ct, nonce, id); err != nil {
		return 0, err
	}
	if passphrase != "" {
		ct, nonce, err := m.Seal([]byte(passphrase), keys.AAD("credential", "passphrase", id))
		if err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE credential SET passphrase_ct = ?, passphrase_nonce = ? WHERE id = ?`,
			ct, nonce, id); err != nil {
			return 0, err
		}
	}
	if certificate != "" {
		ct, nonce, err := m.Seal([]byte(certificate), keys.AAD("credential", "certificate", id))
		if err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE credential SET certificate_ct = ?, certificate_nonce = ? WHERE id = ?`,
			ct, nonce, id); err != nil {
			return 0, err
		}
	}
	return id, tx.Commit()
}

// CreatePasswordCredential seals a username/password profile. Passwords are
// write-only just like private keys and use their own AAD column binding.
func (db *DB) CreatePasswordCredential(ctx context.Context, m *keys.Master, name, username, authKind, password, externalRef string, by *int64) (int64, error) {
	if !PasswordCredentialKinds[authKind] {
		return 0, fmt.Errorf("%q is not a username and password credential type", authKind)
	}
	if username == "" || password == "" {
		return 0, errors.New("a username and password are required")
	}
	tx, err := db.W.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `INSERT INTO credential
		(name, username, auth_kind, private_key_ct, private_key_nonce, external_ref, created_at, created_by)
		VALUES (?,?,?,x'',x'',?,?,?)`, name, username, authKind, externalRef, Now(), by)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	ct, nonce, err := m.Seal([]byte(password), keys.AAD("credential", "password", id))
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE credential SET password_ct = ?, password_nonce = ? WHERE id = ?`, ct, nonce, id); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// UpdateCredential edits an existing profile in place. The auth kind is not
// editable — each kind seals a different column, so "change the kind" is a
// different credential, not an edit of this one.
//
// An empty secret means "keep what is stored": the UI can never show the
// sealed value back, so a blank field has to mean unchanged rather than
// erased. Everything else (name, username, external reference) is plain and
// always overwritten.
func (db *DB) UpdateCredential(ctx context.Context, m *keys.Master, id int64, name, username, privateKey, passphrase, certificate, password, externalRef string) error {
	var kind string
	if err := db.R.QueryRowContext(ctx, `SELECT auth_kind FROM credential WHERE id = ?`, id).Scan(&kind); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("credential %d does not exist", id)
		}
		return err
	}
	if name == "" || username == "" {
		return errors.New("a name and username are required")
	}

	tx, err := db.W.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`UPDATE credential SET name = ?, username = ?, external_ref = ? WHERE id = ?`,
		name, username, externalRef, id); err != nil {
		return err
	}

	seal := func(column, plaintext string) error {
		ct, nonce, err := m.Seal([]byte(plaintext), keys.AAD("credential", column, id))
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx,
			fmt.Sprintf(`UPDATE credential SET %s_ct = ?, %s_nonce = ? WHERE id = ?`, column, column),
			ct, nonce, id)
		return err
	}

	switch kind {
	case "private_key", "ssh_certificate":
		if privateKey != "" {
			signer, err := parseSigner(privateKey, passphrase)
			if err != nil {
				return fmt.Errorf("private key rejected: %w", err)
			}
			if _, err := tx.ExecContext(ctx, `UPDATE credential SET public_key = ?, key_fingerprint = ? WHERE id = ?`,
				string(ssh.MarshalAuthorizedKey(signer.PublicKey())), ssh.FingerprintSHA256(signer.PublicKey()), id); err != nil {
				return err
			}
			if err := seal("private_key", privateKey); err != nil {
				return err
			}
			if err := seal("passphrase", passphrase); err != nil {
				return err
			}
		}
		if certificate != "" {
			if err := seal("certificate", certificate); err != nil {
				return err
			}
		}
	case "cyberark":
		if externalRef == "" {
			return errors.New("a CyberArk account reference is required")
		}
	default:
		if password != "" {
			if err := seal("password", password); err != nil {
				return err
			}
		}
	}

	return tx.Commit()
}

// CreateCyberArkCredential stores a vault account reference, never a copied
// vault secret. A future provider integration can resolve that reference.
func (db *DB) CreateCyberArkCredential(ctx context.Context, name, username, reference string, by *int64) (int64, error) {
	if reference == "" {
		return 0, errors.New("a CyberArk account reference is required")
	}
	res, err := db.W.ExecContext(ctx, `INSERT INTO credential
		(name, username, auth_kind, private_key_ct, private_key_nonce, external_ref, created_at, created_by)
		VALUES (?,?,'cyberark',x'',x'',?,?,?)`, name, username, reference, Now(), by)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// Rekey re-encrypts every stored credential from one Master Key to another and
// returns how many rows it rewrote. The AAD is unchanged — same table, column
// and row id — so this is a straight unseal-and-reseal.
//
// One transaction for the whole set: a partial rekey is a database where some
// credentials open under the old key and some under the new one, and no single
// key can start the server. Either every row moves or none does.
//
// Called by `nagipath rekey`, offline. The Settings · Master key screen prints
// that procedure, and printing a procedure whose tool does not exist is worse
// than not offering rotation at all.
func (db *DB) Rekey(ctx context.Context, old, new *keys.Master) (int, error) {
	rows, err := db.R.QueryContext(ctx, `SELECT id,
		private_key_ct, private_key_nonce, passphrase_ct, passphrase_nonce,
		certificate_ct, certificate_nonce, password_ct, password_nonce FROM credential ORDER BY id`)
	if err != nil {
		return 0, err
	}
	type sealed struct {
		id   int64
		ct   [4][]byte
		once [4][]byte
	}
	var all []sealed
	for rows.Next() {
		var s sealed
		if err := rows.Scan(&s.id, &s.ct[0], &s.once[0], &s.ct[1], &s.once[1],
			&s.ct[2], &s.once[2], &s.ct[3], &s.once[3]); err != nil {
			rows.Close()
			return 0, err
		}
		all = append(all, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	tx, err := db.W.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	cols := [4]string{"private_key", "passphrase", "certificate", "password"}
	for _, s := range all {
		for i, col := range cols {
			// An absent passphrase or certificate is stored as no bytes at all, not as
			// a ciphertext of "", so there is nothing to move.
			if len(s.ct[i]) == 0 {
				continue
			}
			aad := keys.AAD("credential", col, s.id)
			pt, err := old.Open(s.ct[i], s.once[i], aad)
			if err != nil {
				return 0, fmt.Errorf("credential %d %s: %w (is --old the key this database was written with?)", s.id, col, err)
			}
			ct, nonce, err := new.Seal(pt, aad)
			if err != nil {
				return 0, err
			}
			if _, err := tx.ExecContext(ctx, fmt.Sprintf(
				`UPDATE credential SET %s_ct = ?, %s_nonce = ? WHERE id = ?`, col, col),
				ct, nonce, s.id); err != nil {
				return 0, err
			}
		}
	}
	return len(all), tx.Commit()
}

const credentialCols = `id, name, username, auth_kind, public_key, key_fingerprint, external_ref, created_at`

func scanCredential(row interface{ Scan(...any) error }) (Credential, error) {
	var c Credential
	err := row.Scan(&c.ID, &c.Name, &c.Username, &c.AuthKind, &c.PublicKey, &c.Fingerprint, &c.ExternalRef, &c.CreatedAt)
	return c, err
}

func (db *DB) Credentials(ctx context.Context) ([]Credential, error) {
	rows, err := db.R.QueryContext(ctx, `SELECT `+credentialCols+` FROM credential ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Credential
	for rows.Next() {
		c, err := scanCredential(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Signer decrypts a credential and returns an ssh.Signer. The private key never
// leaves this function as a value the caller can print.
func (db *DB) Signer(ctx context.Context, m *keys.Master, id int64) (string, ssh.Signer, error) {
	var (
		username, authKind string
		keyCT, keyNonce    []byte
		passCT, passNonce  []byte
		certCT, certNonce  []byte
	)
	err := db.R.QueryRowContext(ctx, `SELECT username, auth_kind,
		private_key_ct, private_key_nonce, passphrase_ct, passphrase_nonce,
		certificate_ct, certificate_nonce FROM credential WHERE id = ?`, id).
		Scan(&username, &authKind, &keyCT, &keyNonce, &passCT, &passNonce, &certCT, &certNonce)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, fmt.Errorf("credential %d does not exist", id)
	}
	if err != nil {
		return "", nil, err
	}

	pk, err := m.Open(keyCT, keyNonce, keys.AAD("credential", "private_key", id))
	if err != nil {
		return "", nil, fmt.Errorf("credential %d: %w", id, err)
	}
	var pass []byte
	if len(passCT) > 0 {
		pass, err = m.Open(passCT, passNonce, keys.AAD("credential", "passphrase", id))
		if err != nil {
			return "", nil, fmt.Errorf("credential %d passphrase: %w", id, err)
		}
	}
	signer, err := parseSigner(string(pk), string(pass))
	if err != nil {
		return "", nil, err
	}
	if authKind == "ssh_certificate" && len(certCT) > 0 {
		certPEM, err := m.Open(certCT, certNonce, keys.AAD("credential", "certificate", id))
		if err != nil {
			return "", nil, err
		}
		pub, _, _, _, err := ssh.ParseAuthorizedKey(certPEM)
		if err != nil {
			return "", nil, fmt.Errorf("credential %d certificate: %w", id, err)
		}
		cert, ok := pub.(*ssh.Certificate)
		if !ok {
			return "", nil, fmt.Errorf("credential %d: stored certificate is not an SSH certificate", id)
		}
		signer, err = ssh.NewCertSigner(cert, signer)
		if err != nil {
			return "", nil, err
		}
	}
	return username, signer, nil
}

// SSHPassword decrypts a credential only when it is a password-backed SSH
// profile. Whether the account is local or comes from a directory is the
// managed host's business (its PAM/nsswitch), not a distinction this side
// can act on — hence one password kind, not one per directory.
func (db *DB) SSHPassword(ctx context.Context, m *keys.Master, id int64) (string, string, error) {
	var username, authKind string
	var ct, nonce []byte
	err := db.R.QueryRowContext(ctx, `SELECT username, auth_kind, password_ct, password_nonce FROM credential WHERE id = ?`, id).
		Scan(&username, &authKind, &ct, &nonce)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", fmt.Errorf("credential %d does not exist", id)
	}
	if err != nil {
		return "", "", err
	}
	if authKind != "username_password" {
		return "", "", fmt.Errorf("credential %d (%s) cannot authenticate to SSH with a password", id, authKind)
	}
	password, err := m.Open(ct, nonce, keys.AAD("credential", "password", id))
	if err != nil {
		return "", "", fmt.Errorf("credential %d password: %w", id, err)
	}
	return username, string(password), nil
}

// CredentialKind is used to select the SSH authentication mechanism without
// exposing a secret.
func (db *DB) CredentialKind(ctx context.Context, id int64) (string, error) {
	var kind string
	err := db.R.QueryRowContext(ctx, `SELECT auth_kind FROM credential WHERE id = ?`, id).Scan(&kind)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("credential %d does not exist", id)
	}
	return kind, err
}

func parseSigner(privateKey, passphrase string) (ssh.Signer, error) {
	if passphrase != "" {
		return ssh.ParsePrivateKeyWithPassphrase([]byte(privateKey), []byte(passphrase))
	}
	return ssh.ParsePrivateKey([]byte(privateKey))
}

// ResolveCredential implements the resolution order in ADR-0011: the Node's own
// Credential, then the Cluster of any Instance on it, then the fleet default.
// The two accepted holes are documented in that ADR's Consequences — the Cluster
// tier cannot apply on first contact, and a Node spanning two Clusters falls
// through to the default rather than inventing a plurality winner.
func (db *DB) ResolveCredential(ctx context.Context, nodeID int64) (int64, error) {
	var id sql.NullInt64
	if err := db.R.QueryRowContext(ctx,
		`SELECT credential_id FROM node WHERE id = ?`, nodeID).Scan(&id); err != nil {
		return 0, err
	}
	if id.Valid {
		return id.Int64, nil
	}
	if err := db.R.QueryRowContext(ctx, `SELECT c.credential_id
		FROM instance i JOIN cluster c ON c.id = i.cluster_id
		WHERE i.node_id = ? AND i.retired_at IS NULL AND c.credential_id IS NOT NULL
		GROUP BY c.credential_id
		HAVING COUNT(*) = (SELECT COUNT(*) FROM instance i2 JOIN cluster c2 ON c2.id = i2.cluster_id
		                   WHERE i2.node_id = ? AND i2.retired_at IS NULL AND c2.credential_id IS NOT NULL)`,
		nodeID, nodeID).Scan(&id); err == nil && id.Valid {
		return id.Int64, nil
	}
	if v := db.Setting(ctx, "default_credential_id"); v != "" {
		var n int64
		if _, err := fmt.Sscan(v, &n); err == nil && n > 0 {
			return n, nil
		}
	}
	return 0, ErrNoCredential
}

// FingerprintSHA256 of an authorized-keys line, for display next to a credential.
func FingerprintOf(authorizedKey string) string {
	pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(authorizedKey))
	if err != nil {
		sum := sha256.Sum256([]byte(authorizedKey))
		return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
	}
	return ssh.FingerprintSHA256(pub)
}
