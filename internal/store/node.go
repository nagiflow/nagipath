package store

import (
	"context"
	"database/sql"
	"fmt"
)

type Node struct {
	ID                  int64
	Address             string
	SSHPort             int
	DisplayName         string
	SSHUsername         sql.NullString
	CredentialID        sql.NullInt64
	BastionNodeID       sql.NullInt64
	OSFamily            string
	SudoAvailable       bool
	Enabled             bool
	Source              string
	Notes               string
	ConsecutiveFailures int
	FirstSeenAt         string
	RetiredAt           sql.NullString

	// Joined for the list view.
	InstanceCount   int
	PendingHostKeys int
	LastCollection  sql.NullString
	LastStatus      sql.NullString
}

const nodeCols = `id, address, ssh_port, display_name, ssh_username, credential_id,
	bastion_node_id, os_family, sudo_available, enabled, source, notes,
	consecutive_failures, first_seen_at, retired_at`

func scanNode(sc interface{ Scan(...any) error }) (Node, error) {
	var n Node
	err := sc.Scan(&n.ID, &n.Address, &n.SSHPort, &n.DisplayName, &n.SSHUsername,
		&n.CredentialID, &n.BastionNodeID, &n.OSFamily, &n.SudoAvailable, &n.Enabled,
		&n.Source, &n.Notes, &n.ConsecutiveFailures, &n.FirstSeenAt, &n.RetiredAt)
	return n, err
}

func (db *DB) AddNode(ctx context.Context, address string, port int, displayName, username string, credentialID *int64, bastion *int64, source string, by *int64) (int64, error) {
	if displayName == "" {
		displayName = address
	}
	if port == 0 {
		port = 22
	}
	tx, err := db.W.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `INSERT INTO node
		(address, ssh_port, display_name, ssh_username, credential_id, bastion_node_id,
		 source, first_seen_at)
		VALUES (?,?,?,?,?,?,?,?)`,
		address, port, displayName, NullString(username), credentialID, bastion, source, Now())
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err := auditTx(ctx, tx, by, "node.added", "node", &id, address); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

func (db *DB) Node(ctx context.Context, id int64) (Node, error) {
	return scanNode(db.R.QueryRowContext(ctx, `SELECT `+nodeCols+` FROM node WHERE id = ?`, id))
}

// NodeCount is the number of non-retired Nodes, i.e. what a license's node
// ceiling is measured against.
func (db *DB) NodeCount(ctx context.Context) (int, error) {
	var n int
	err := db.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM node WHERE retired_at IS NULL`).Scan(&n)
	return n, err
}

// Nodes returns every non-retired Node with the counts the list view shows.
func (db *DB) Nodes(ctx context.Context) ([]Node, error) {
	rows, err := db.R.QueryContext(ctx, `SELECT `+nodeCols+`,
		(SELECT COUNT(*) FROM instance i WHERE i.node_id = node.id AND i.retired_at IS NULL),
		(SELECT COUNT(*) FROM host_key h WHERE h.node_id = node.id AND h.state = 'pending'),
		(SELECT MAX(started_at) FROM collection c WHERE c.node_id = node.id),
		(SELECT status FROM collection c WHERE c.node_id = node.id ORDER BY started_at DESC LIMIT 1)
		FROM node WHERE retired_at IS NULL ORDER BY display_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Node
	for rows.Next() {
		var n Node
		if err := rows.Scan(&n.ID, &n.Address, &n.SSHPort, &n.DisplayName, &n.SSHUsername,
			&n.CredentialID, &n.BastionNodeID, &n.OSFamily, &n.SudoAvailable, &n.Enabled,
			&n.Source, &n.Notes, &n.ConsecutiveFailures, &n.FirstSeenAt, &n.RetiredAt,
			&n.InstanceCount, &n.PendingHostKeys, &n.LastCollection, &n.LastStatus); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (db *DB) SetNodeFacts(ctx context.Context, id int64, osFamily string, sudo bool) error {
	_, err := db.W.ExecContext(ctx,
		`UPDATE node SET os_family = ?, sudo_available = ? WHERE id = ?`, osFamily, sudo, id)
	return err
}

// Quarantined reports whether a Node's consecutive collection failures have
// reached the operator's configured tolerance (the quarantine_after_failures
// setting). It is a label only — nagipath reports that a Node has failed
// beyond that tolerance, it does not skip or back off collection because of
// it (see schema.md and CONTEXT.md's treatment of Drift for the same
// posture: surfaced honestly, never acted on autonomously).
func Quarantined(consecutiveFailures, threshold int) bool {
	// A threshold of zero is a missing setting, not a fleet where every node is
	// quarantined the moment it is added.
	return threshold > 0 && consecutiveFailures >= threshold
}

// QuarantinedNodeCount is the number of non-retired Nodes whose consecutive
// failures have reached threshold, computed in SQL so /metrics does not have
// to pull every Node into Go just to count them.
func (db *DB) QuarantinedNodeCount(ctx context.Context, threshold int) (int, error) {
	var n int
	err := db.R.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM node WHERE retired_at IS NULL AND consecutive_failures >= ?`,
		threshold).Scan(&n)
	return n, err
}

func (db *DB) NodeFailure(ctx context.Context, id int64, failed bool) error {
	if failed {
		_, err := db.W.ExecContext(ctx,
			`UPDATE node SET consecutive_failures = consecutive_failures + 1 WHERE id = ?`, id)
		return err
	}
	_, err := db.W.ExecContext(ctx, `UPDATE node SET consecutive_failures = 0 WHERE id = ?`, id)
	return err
}

func (db *DB) SetNodeCredential(ctx context.Context, id, credentialID int64, by *int64) error {
	tx, err := db.W.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var address string
	if err := tx.QueryRowContext(ctx, `SELECT address FROM node WHERE id = ?`, id).Scan(&address); err != nil {
		return err
	}
	var name string
	if err := tx.QueryRowContext(ctx, `SELECT name FROM credential WHERE id = ?`, credentialID).Scan(&name); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE node SET credential_id = ? WHERE id = ?`, credentialID, id); err != nil {
		return err
	}
	if err := auditTx(ctx, tx, by, "node.credential_changed", "node", &id,
		fmt.Sprintf("%s: %s", address, name)); err != nil {
		return err
	}
	return tx.Commit()
}

func (db *DB) DeleteNode(ctx context.Context, id int64, by *int64) error {
	tx, err := db.W.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var address string
	tx.QueryRowContext(ctx, `SELECT address FROM node WHERE id = ?`, id).Scan(&address)
	if _, err := tx.ExecContext(ctx, `DELETE FROM node WHERE id = ?`, id); err != nil {
		return err
	}
	if err := auditTx(ctx, tx, by, "node.deleted", "node", &id, address); err != nil {
		return err
	}
	return tx.Commit()
}
