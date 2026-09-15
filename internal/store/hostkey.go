package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ErrHostKeyPending is returned when a Node presents a key nobody has approved
// and the tofu_enabled setting is off (the default). It is not a transient
// failure: the collection stops here until an operator decides. A key that
// would replace an already-approved one is never auto-approved regardless of
// tofu_enabled — see CheckHostKey.
type ErrHostKeyPending struct {
	NodeID      int64
	Algorithm   string
	Fingerprint string
}

func (e *ErrHostKeyPending) Error() string {
	return fmt.Sprintf("host key awaiting approval: node %d presented %s %s",
		e.NodeID, e.Algorithm, e.Fingerprint)
}

// ErrHostKeyMismatch means an approved key exists for this algorithm and the
// host presented a different one. Louder than pending, and never auto-resolved.
type ErrHostKeyMismatch struct {
	NodeID    int64
	Algorithm string
	Approved  string
	Presented string
}

func (e *ErrHostKeyMismatch) Error() string {
	return fmt.Sprintf("host key MISMATCH on node %d (%s): approved %s, presented %s",
		e.NodeID, e.Algorithm, e.Approved, e.Presented)
}

type HostKey struct {
	ID          int64
	NodeID      int64
	Algorithm   string
	PublicKey   string
	Fingerprint string
	State       string
	FirstSeenAt string
	DecidedAt   sql.NullString
}

// CheckHostKey is the whole trust decision. Called from the ssh.HostKeyCallback.
// A key that would replace one already approved for this node+algorithm is
// always a mismatch requiring a manual decision — tofu_enabled only ever
// applies to a node's first-ever key for that algorithm.
func (db *DB) CheckHostKey(ctx context.Context, nodeID int64, algorithm, publicKey, fingerprint string) error {
	var state string
	err := db.R.QueryRowContext(ctx,
		`SELECT state FROM host_key WHERE node_id = ? AND algorithm = ? AND fingerprint = ?`,
		nodeID, algorithm, fingerprint).Scan(&state)
	switch {
	case err == nil && state == "approved":
		return nil
	case err == nil && state == "rejected":
		return fmt.Errorf("host key for node %d was rejected by an operator", nodeID)
	case err == nil:
		return &ErrHostKeyPending{nodeID, algorithm, fingerprint}
	case !errors.Is(err, sql.ErrNoRows):
		return err
	}

	// Unknown key. If another key of the same algorithm is already approved,
	// this is a mismatch, which is a different and more serious event.
	var approved string
	switch err := db.R.QueryRowContext(ctx,
		`SELECT fingerprint FROM host_key
		 WHERE node_id = ? AND algorithm = ? AND state = 'approved'`,
		nodeID, algorithm).Scan(&approved); {
	case err == nil:
		if _, err := db.W.ExecContext(ctx, `INSERT OR IGNORE INTO host_key
			(node_id, algorithm, public_key, fingerprint, state, first_seen_at)
			VALUES (?,?,?,?,'pending',?)`, nodeID, algorithm, publicKey, fingerprint, Now()); err != nil {
			return err
		}
		return &ErrHostKeyMismatch{nodeID, algorithm, approved, fingerprint}
	case !errors.Is(err, sql.ErrNoRows):
		return err
	}

	if db.SettingBool(ctx, "tofu_enabled") {
		tx, err := db.W.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO host_key
			(node_id, algorithm, public_key, fingerprint, state, first_seen_at, decided_at)
			VALUES (?,?,?,?,'approved',?,?)`, nodeID, algorithm, publicKey, fingerprint, Now(), Now()); err != nil {
			return err
		}
		if err := auditTx(ctx, tx, nil, "host_key.tofu_approved", "node", &nodeID,
			fmt.Sprintf("%s %s", algorithm, fingerprint)); err != nil {
			return err
		}
		return tx.Commit()
	}

	if _, err := db.W.ExecContext(ctx, `INSERT OR IGNORE INTO host_key
		(node_id, algorithm, public_key, fingerprint, state, first_seen_at)
		VALUES (?,?,?,?,'pending',?)`, nodeID, algorithm, publicKey, fingerprint, Now()); err != nil {
		return err
	}
	return &ErrHostKeyPending{nodeID, algorithm, fingerprint}
}

func (db *DB) HostKeys(ctx context.Context, nodeID int64) ([]HostKey, error) {
	rows, err := db.R.QueryContext(ctx, `SELECT id, node_id, algorithm, public_key,
		fingerprint, state, first_seen_at, decided_at FROM host_key
		WHERE node_id = ? ORDER BY first_seen_at DESC`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HostKey
	for rows.Next() {
		var h HostKey
		if err := rows.Scan(&h.ID, &h.NodeID, &h.Algorithm, &h.PublicKey,
			&h.Fingerprint, &h.State, &h.FirstSeenAt, &h.DecidedAt); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// PendingHostKey is one key waiting for a human. NodeName and NodeAddress are
// joined in because approving a fingerprint you cannot attribute to a host is
// approving nothing.
type PendingHostKey struct {
	HostKey
	NodeName    string
	NodeAddress string
	// Previous is the fingerprint already approved for the same algorithm, if any.
	// A key that replaces one is a rekey or an interception, and the operator
	// cannot tell which without seeing both.
	Previous string
	// Cluster names every cluster this node's processes sit in, comma-separated.
	// Only AllHostKeys fills it — the node page already knows its own clusters.
	Cluster string
}

func (db *DB) PendingHostKeys(ctx context.Context) ([]PendingHostKey, error) {
	rows, err := db.R.QueryContext(ctx, `SELECT h.id, h.node_id, h.algorithm, h.public_key,
		h.fingerprint, h.state, h.first_seen_at, h.decided_at, n.display_name, n.address,
		COALESCE((SELECT p.fingerprint FROM host_key p WHERE p.node_id = h.node_id
		   AND p.algorithm = h.algorithm AND p.state = 'approved' LIMIT 1), '')
		FROM host_key h JOIN node n ON n.id = h.node_id
		WHERE h.state = 'pending' ORDER BY n.display_name, h.algorithm`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PendingHostKey
	for rows.Next() {
		var p PendingHostKey
		if err := rows.Scan(&p.ID, &p.NodeID, &p.Algorithm, &p.PublicKey, &p.Fingerprint,
			&p.State, &p.FirstSeenAt, &p.DecidedAt, &p.NodeName, &p.NodeAddress,
			&p.Previous); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (db *DB) PendingHostKeyCount(ctx context.Context) int {
	var n int
	db.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM host_key WHERE state = 'pending'`).Scan(&n)
	return n
}

// DecideHostKey approves or rejects. Approving supersedes any previously
// approved key for the same algorithm, so approval is also how a legitimate
// rekey is accepted.
func (db *DB) DecideHostKey(ctx context.Context, id int64, approve bool, by *int64) error {
	tx, err := db.W.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var nodeID int64
	var algorithm, fingerprint string
	if err := tx.QueryRowContext(ctx,
		`SELECT node_id, algorithm, fingerprint FROM host_key WHERE id = ?`, id).
		Scan(&nodeID, &algorithm, &fingerprint); err != nil {
		return err
	}
	state := "rejected"
	if approve {
		state = "approved"
		if _, err := tx.ExecContext(ctx, `UPDATE host_key
			SET state = 'superseded', superseded_at = ?
			WHERE node_id = ? AND algorithm = ? AND state = 'approved' AND id != ?`,
			Now(), nodeID, algorithm, id); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE host_key SET state = ?, decided_at = ?, decided_by = ? WHERE id = ?`,
		state, Now(), by, id); err != nil {
		return err
	}
	if err := auditTx(ctx, tx, by, "host_key."+state, "node", &nodeID,
		fmt.Sprintf("%s %s", algorithm, fingerprint)); err != nil {
		return err
	}
	return tx.Commit()
}
