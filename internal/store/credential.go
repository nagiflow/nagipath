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
	CreatedAt   string
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

const credentialCols = `id, name, username, auth_kind, public_key, key_fingerprint, created_at`

func scanCredential(row interface{ Scan(...any) error }) (Credential, error) {
	var c Credential
	err := row.Scan(&c.ID, &c.Name, &c.Username, &c.AuthKind, &c.PublicKey, &c.Fingerprint, &c.CreatedAt)
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
