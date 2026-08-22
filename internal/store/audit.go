package store

import (
	"context"
	"database/sql"
	"encoding/json"
)

// Audit writes one immutable event. Anything an operator does that changes state
// on a Node, a Credential or a Probe goes through here.
func (db *DB) Audit(ctx context.Context, actor *int64, action, targetKind string, targetID *int64, targetLabel string) error {
	tx, err := db.W.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := auditTx(ctx, tx, actor, action, targetKind, targetID, targetLabel); err != nil {
		return err
	}
	return tx.Commit()
}

func auditTx(ctx context.Context, tx *sql.Tx, actor *int64, action, targetKind string, targetID *int64, targetLabel string) error {
	label := "system"
	if actor != nil {
		if err := tx.QueryRowContext(ctx, `SELECT username FROM app_user WHERE id = ?`, actor).
			Scan(&label); err != nil {
			label = "user"
		}
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO audit_event
		(at, actor_user_id, actor_label, action, target_kind, target_id, target_label, outcome)
		VALUES (?,?,?,?,?,?,?, 'success')`,
		Now(), actor, label, action, targetKind, targetID, targetLabel)
	return err
}

// AuditDetail is for the events where the "what exactly" matters — Probe targets,
// setting changes, credential rotation.
func (db *DB) AuditDetail(ctx context.Context, actor *int64, action, targetKind string, targetID *int64, targetLabel string, detail map[string]any, outcome, remoteAddr string) error {
	blob, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	label := "system"
	if actor != nil {
		db.R.QueryRowContext(ctx, `SELECT username FROM app_user WHERE id = ?`, actor).Scan(&label)
	}
	_, err = db.W.ExecContext(ctx, `INSERT INTO audit_event
		(at, actor_user_id, actor_label, action, target_kind, target_id, target_label,
		 outcome, detail, remote_addr)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		Now(), actor, label, action, targetKind, targetID, targetLabel,
		outcome, string(blob), remoteAddr)
	return err
}

type AuditEvent struct {
	At          string
	ActorLabel  string
	Action      string
	TargetKind  string
	TargetLabel string
	Outcome     string
	Detail      string
}

func (db *DB) AuditEvents(ctx context.Context, limit int) ([]AuditEvent, error) {
	rows, err := db.R.QueryContext(ctx, `SELECT at, actor_label, action, target_kind,
		target_label, outcome, detail FROM audit_event ORDER BY at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEvent
	for rows.Next() {
		var e AuditEvent
		if err := rows.Scan(&e.At, &e.ActorLabel, &e.Action, &e.TargetKind,
			&e.TargetLabel, &e.Outcome, &e.Detail); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
