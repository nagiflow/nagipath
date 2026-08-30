package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
)

// Certificate is metadata only. Nothing in this struct can reconstruct a private
// key, because the extraction happens on the target with `openssl x509 -noout`
// and the key file is never read (PRD-V1 security constraints).
type Certificate struct {
	ID           int64
	Fingerprint  string
	SubjectCN    string
	SubjectDN    string
	SANs         []string
	IssuerDN     string
	Serial       string
	NotBefore    string
	NotAfter     string
	KeyAlgorithm string
	KeyBits      sql.NullInt64
	SigAlgorithm string
	SelfSigned   bool
	IsCA         bool
	FirstSeenAt  string
	LastSeenAt   string
}

func (db *DB) UpsertCertificate(ctx context.Context, c Certificate) (int64, error) {
	sans, err := json.Marshal(c.SANs)
	if err != nil {
		return 0, err
	}
	if c.SANs == nil {
		sans = []byte("[]")
	}
	now := Now()
	_, err = db.W.ExecContext(ctx, `INSERT INTO certificate
		(fingerprint_sha256, subject_cn, subject_dn, sans, issuer_dn, serial,
		 not_before, not_after, key_algorithm, key_bits, signature_algorithm,
		 is_self_signed, is_ca, first_seen_at, last_seen_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(fingerprint_sha256) DO UPDATE SET last_seen_at = excluded.last_seen_at`,
		c.Fingerprint, c.SubjectCN, c.SubjectDN, string(sans), c.IssuerDN, c.Serial,
		c.NotBefore, c.NotAfter, c.KeyAlgorithm, c.KeyBits, c.SigAlgorithm,
		boolInt(c.SelfSigned), boolInt(c.IsCA), now, now)
	if err != nil {
		return 0, err
	}
	var id int64
	err = db.R.QueryRowContext(ctx,
		`SELECT id FROM certificate WHERE fingerprint_sha256 = ?`, c.Fingerprint).Scan(&id)
	return id, err
}

type CertBindingRow struct {
	CertificateID int64
	InstanceID    int64
	SnapshotID    int64
	SiteID        *int64
	ListenerID    *int64
	FilePath      string
	CombinedPEM   bool
	ProvFileID    *int64
	ProvStart     *int
	ProvEnd       *int
}

func (db *DB) AddCertBinding(ctx context.Context, b CertBindingRow) error {
	_, err := db.W.ExecContext(ctx, `INSERT OR REPLACE INTO certificate_binding
		(certificate_id, instance_id, snapshot_id, listener_id, site_id, file_path,
		 combined_pem, prov_file_id, prov_byte_start, prov_byte_end, observed_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		b.CertificateID, b.InstanceID, b.SnapshotID, b.ListenerID, b.SiteID, b.FilePath,
		boolInt(b.CombinedPEM), b.ProvFileID, b.ProvStart, b.ProvEnd, Now())
	return err
}

// CertificateView is one row of the expiry-oriented certificate list.
type CertificateView struct {
	Certificate
	Bindings int
	Hosts    string
	// Serves are the hostnames of the sites this certificate is bound to. A
	// certificate that does not cover the name it is serving is a working
	// configuration that fails in every browser, so the two have to be visible
	// together to be comparable.
	Serves []string
}

// CertBinding is one place a certificate is actually in use. The selected-
// certificate panel needs this: "expires in 12 days" is only actionable next to
// the list of things that stop working when it does.
type CertBinding struct {
	InstanceID  int64
	Instance    string
	NodeID      int64
	Node        string
	ClusterName string
	SnapshotID  int64
	FileID      int64
	FilePath    string
	SiteNames   string
	Port        int
	CombinedPEM bool
}

func (db *DB) CertBindings(ctx context.Context, certID int64) ([]CertBinding, error) {
	rows, err := db.R.QueryContext(ctx, `SELECT b.instance_id, i.display_name,
		n.id, n.display_name, COALESCE(c.name, ''), b.snapshot_id, COALESCE(b.prov_file_id, 0), b.file_path,
		COALESCE((SELECT group_concat(DISTINCT sn.name) FROM site_name sn
		   WHERE sn.site_id = b.site_id), ''),
		COALESCE((SELECT l.port FROM listener l WHERE l.id = b.listener_id), 0),
		b.combined_pem
		FROM certificate_binding b
		JOIN snapshot s ON s.id = b.snapshot_id AND s.is_current = 1
		JOIN instance i ON i.id = b.instance_id
		JOIN node n ON n.id = i.node_id
		LEFT JOIN cluster c ON c.id = i.cluster_id
		WHERE b.certificate_id = ?
		ORDER BY i.display_name, b.file_path`, certID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CertBinding
	for rows.Next() {
		var b CertBinding
		if err := rows.Scan(&b.InstanceID, &b.Instance, &b.NodeID, &b.Node, &b.ClusterName, &b.SnapshotID, &b.FileID,
			&b.FilePath, &b.SiteNames, &b.Port, &b.CombinedPEM); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (db *DB) CertificateByID(ctx context.Context, id int64) (Certificate, error) {
	var c Certificate
	var sans string
	err := db.R.QueryRowContext(ctx, `SELECT id, fingerprint_sha256, subject_cn, subject_dn,
		sans, issuer_dn, serial, not_before, not_after, key_algorithm, key_bits,
		signature_algorithm, is_self_signed, is_ca, first_seen_at, last_seen_at
		FROM certificate WHERE id = ?`, id).Scan(&c.ID, &c.Fingerprint, &c.SubjectCN,
		&c.SubjectDN, &sans, &c.IssuerDN, &c.Serial, &c.NotBefore, &c.NotAfter,
		&c.KeyAlgorithm, &c.KeyBits, &c.SigAlgorithm, &c.SelfSigned, &c.IsCA,
		&c.FirstSeenAt, &c.LastSeenAt)
	if err != nil {
		return Certificate{}, err
	}
	json.Unmarshal([]byte(sans), &c.SANs)
	return c, nil
}

func (db *DB) Certificates(ctx context.Context) ([]CertificateView, error) {
	rows, err := db.R.QueryContext(ctx, `SELECT c.id, c.fingerprint_sha256, c.subject_cn,
		c.sans, c.issuer_dn, c.not_before, c.not_after, c.is_self_signed, c.last_seen_at,
		c.key_algorithm, c.key_bits,
		(SELECT COUNT(*) FROM certificate_binding b
		   JOIN snapshot s ON s.id = b.snapshot_id
		   WHERE b.certificate_id = c.id AND s.is_current = 1),
		COALESCE((SELECT group_concat(DISTINCT n.display_name) FROM certificate_binding b
		   JOIN snapshot s ON s.id = b.snapshot_id
		   JOIN instance i ON i.id = b.instance_id
		   JOIN node n ON n.id = i.node_id
		   WHERE b.certificate_id = c.id AND s.is_current = 1), ''),
		COALESCE((SELECT group_concat(DISTINCT sn.name) FROM certificate_binding b
		   JOIN snapshot s ON s.id = b.snapshot_id
		   JOIN site_name sn ON sn.site_id = b.site_id
		   WHERE b.certificate_id = c.id AND s.is_current = 1
		     AND sn.match_kind != 'catch_all'), '')
		FROM certificate c ORDER BY c.not_after`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CertificateView
	for rows.Next() {
		var v CertificateView
		var sans, serves string
		// The key columns were stored and never read back, so the list's Key column
		// was blank on every row of a real install.
		if err := rows.Scan(&v.ID, &v.Fingerprint, &v.SubjectCN, &sans, &v.IssuerDN,
			&v.NotBefore, &v.NotAfter, &v.SelfSigned, &v.LastSeenAt,
			&v.KeyAlgorithm, &v.KeyBits,
			&v.Bindings, &v.Hosts, &serves); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(sans), &v.SANs)
		if serves != "" {
			v.Serves = strings.Split(serves, ",")
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
