package store

import (
	"context"
	"fmt"
	"time"
)

// Retention. The Settings screen states four windows and a rule about drift
// history; this is the code that makes those statements true.
//
// Order matters and is not interchangeable:
//
//  1. Snapshots first. Deleting a collection cascades to its snapshots, so
//     pruning job logs before snapshots would take a 90-day snapshot out with a
//     30-day log line.
//  2. Job logs (collection rows) only once they hold no snapshot at all. A
//     collection row is the log of a run; while any of its snapshots is still
//     retained, that log is the provenance of retained data.
//  3. Probes and audit events are independent of both.
//  4. Blobs last, via GCBlobs: content is shared between snapshots, so a blob is
//     only orphaned after the last snapshot_file referencing it is gone.

// Pruned is what one retention pass removed, for the log line and the tests.
type Pruned struct {
	Snapshots, JobLogs, Probes, AuditEvents, Blobs int64
}

func (p Pruned) Any() bool {
	return p.Snapshots+p.JobLogs+p.Probes+p.AuditEvents+p.Blobs > 0
}

// Prune enforces the configured retention windows. It is safe to call on a live
// database and safe to call repeatedly — a second pass in the same hour finds
// nothing left to do.
//
// A window of 0 means "keep forever" for that kind, so an operator who does not
// want their audit trail pruned has a way to say it.
func (db *DB) Prune(ctx context.Context) (Pruned, error) {
	var out Pruned
	now := time.Now().UTC()
	cutoff := func(key string) (string, bool) {
		days := db.SettingInt(ctx, key)
		if days <= 0 {
			return "", false
		}
		return now.AddDate(0, 0, -days).Format("2006-01-02T15:04:05Z"), true
	}

	if before, ok := cutoff("snapshot_retention_days"); ok {
		// Three exemptions, each one a thing an operator would be right to be angry
		// about losing:
		//   - the current snapshot of an instance, which is what every screen reads;
		//   - the newest N per instance, so an instance collected once a year still
		//     has a history to compare against;
		//   - any snapshot a drift run with findings points at, subject or baseline,
		//     because that pair IS the recorded change. Deleting either half turns
		//     "web05 diverged on Aug 19" into nothing at all.
		keep := db.SettingInt(ctx, "snapshot_retention_min_per_instance")
		res, err := db.W.ExecContext(ctx, `DELETE FROM snapshot WHERE captured_at < ?
			AND is_current = 0
			AND id NOT IN (
				SELECT id FROM (
					SELECT id, ROW_NUMBER() OVER (PARTITION BY instance_id ORDER BY captured_at DESC) AS n
					FROM snapshot) WHERE n <= ?)
			AND id NOT IN (SELECT subject_snapshot_id FROM drift_run WHERE finding_count > 0)
			AND id NOT IN (SELECT baseline_snapshot_id FROM drift_run
			               WHERE finding_count > 0 AND baseline_snapshot_id IS NOT NULL)`,
			before, keep)
		if err != nil {
			return out, fmt.Errorf("prune snapshots: %w", err)
		}
		out.Snapshots, _ = res.RowsAffected()
	}

	if before, ok := cutoff("job_log_retention_days"); ok {
		res, err := db.W.ExecContext(ctx, `DELETE FROM collection
			WHERE started_at < ? AND status <> 'running'
			AND id NOT IN (SELECT collection_id FROM snapshot)`, before)
		if err != nil {
			return out, fmt.Errorf("prune job logs: %w", err)
		}
		out.JobLogs, _ = res.RowsAffected()
	}

	if before, ok := cutoff("probe_retention_days"); ok {
		// probe_evidence cascades. A probe still running is never old enough to
		// matter, and deleting it mid-flight would lose the record of a request that
		// has already gone out.
		res, err := db.W.ExecContext(ctx,
			`DELETE FROM probe WHERE requested_at < ? AND result <> 'running'`, before)
		if err != nil {
			return out, fmt.Errorf("prune probes: %w", err)
		}
		out.Probes, _ = res.RowsAffected()
	}

	if before, ok := cutoff("audit_retention_days"); ok {
		res, err := db.W.ExecContext(ctx, `DELETE FROM audit_event WHERE at < ?`, before)
		if err != nil {
			return out, fmt.Errorf("prune audit events: %w", err)
		}
		out.AuditEvents, _ = res.RowsAffected()
	}

	n, err := db.GCBlobs(ctx)
	if err != nil {
		return out, fmt.Errorf("blob gc: %w", err)
	}
	out.Blobs = n
	return out, nil
}
