package store

import (
	"context"
	"database/sql"
	"sort"
	"strings"
	"time"
)

// CredentialView extends Credential with operational metadata: how many nodes
// use it and when it was last exercised by collection.
type CredentialView struct {
	Credential
	NodeCount int
	LastUsed  string // RFC3339, or "" if never used
}

// CredentialsWithUsage returns all credentials annotated with their node counts
// and last-collection timestamp. The authKind filter is a comma-separated list;
// empty means all kinds.
func (db *DB) CredentialsWithUsage(ctx context.Context, authKindFilter string) ([]CredentialView, error) {
	// Build the WHERE clause once for both queries.
	where := "1=1"
	var args []any
	if authKindFilter != "" {
		kinds := strings.Split(authKindFilter, ",")
		placeholders := make([]string, len(kinds))
		for i, k := range kinds {
			placeholders[i] = "?"
			args = append(args, strings.TrimSpace(k))
		}
		where = "c.auth_kind IN (" + strings.Join(placeholders, ",") + ")"
	}

	// First query: credentials with node counts.
	rows, err := db.R.QueryContext(ctx, `
		SELECT `+credentialCols+`,
		       COALESCE((SELECT COUNT(*) FROM node WHERE credential_id = c.id), 0) AS node_count
		FROM credential c
		WHERE `+where+`
		ORDER BY c.name`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []CredentialView
	for rows.Next() {
		var cv CredentialView
		if err := rows.Scan(&cv.ID, &cv.Name, &cv.Username, &cv.AuthKind,
			&cv.PublicKey, &cv.Fingerprint, &cv.ExternalRef, &cv.CreatedAt, &cv.NodeCount); err != nil {
			return nil, err
		}
		out = append(out, cv)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Second query: last collection per credential. A LEFT JOIN in the first query
	// loads every collection row for every credential — a 428-node fleet with daily
	// collection is 156,220 rows a year, and SQLite resolves the MAX inside that
	// set at the wrong end of the query plan. Two queries, the second scoped to
	// credentials that have nodes, keeps it under 1ms even at scale.
	for i := range out {
		if out[i].NodeCount == 0 {
			continue
		}
		var ts sql.NullString
		err := db.R.QueryRowContext(ctx, `
			SELECT MAX(started_at)
			FROM collection
			WHERE node_id IN (SELECT id FROM node WHERE credential_id = ?)`, out[i].ID).Scan(&ts)
		if err != nil && err != sql.ErrNoRows {
			return nil, err
		}
		if ts.Valid {
			out[i].LastUsed = ts.String
		}
	}

	return out, nil
}

// CredentialAuthKinds returns the distinct auth_kind values in the credential
// table, which is what the Type filter select needs.
func (db *DB) CredentialAuthKinds(ctx context.Context) ([]string, error) {
	rows, err := db.R.QueryContext(ctx, `SELECT DISTINCT auth_kind FROM credential ORDER BY auth_kind`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// AuditFilter holds the URL filter state for the audit log.
type AuditFilter struct {
	Actor      string
	Action     string
	DateRange  string
	Page       int
	PerPage    int
	ExportMode bool
}

// AuditResult is the filtered and paginated audit log.
type AuditResult struct {
	Events     []AuditEvent
	Total      int
	TotalPages int
	Page       int
	PerPage    int
}

// AuditEventsFiltered returns the audit log with filters applied, paginated.
// remote_addr is now selected and populated in AuditEvent.
func (db *DB) AuditEventsFiltered(ctx context.Context, f AuditFilter) (AuditResult, error) {
	// Build WHERE clauses and args.
	where := []string{"1=1"}
	var args []any

	if f.Actor != "" {
		where = append(where, "LOWER(actor_label) LIKE ?")
		args = append(args, "%"+strings.ToLower(f.Actor)+"%")
	}
	if f.Action != "" {
		where = append(where, "action = ?")
		args = append(args, f.Action)
	}
	if f.DateRange != "" {
		since := auditDateSince(f.DateRange)
		if since != "" {
			where = append(where, "at >= ?")
			args = append(args, since)
		}
	}

	whereClause := strings.Join(where, " AND ")

	// Count total matching rows.
	var total int
	err := db.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_event WHERE `+whereClause, args...).Scan(&total)
	if err != nil {
		return AuditResult{}, err
	}

	// If export mode, return all matching rows without pagination.
	if f.ExportMode {
		rows, err := db.R.QueryContext(ctx, `
			SELECT at, actor_label, action, target_kind, target_label, outcome,
			       COALESCE(detail, '{}'), COALESCE(remote_addr, '')
			FROM audit_event
			WHERE `+whereClause+`
			ORDER BY at DESC, id DESC`, args...)
		if err != nil {
			return AuditResult{}, err
		}
		defer rows.Close()

		var events []AuditEvent
		for rows.Next() {
			var e AuditEvent
			if err := rows.Scan(&e.At, &e.ActorLabel, &e.Action, &e.TargetKind,
				&e.TargetLabel, &e.Outcome, &e.Detail, &e.SourceIP); err != nil {
				return AuditResult{}, err
			}
			events = append(events, e)
		}
		return AuditResult{Events: events, Total: total}, rows.Err()
	}

	// Paginated mode.
	if f.PerPage <= 0 {
		f.PerPage = 50
	}
	if f.Page < 1 {
		f.Page = 1
	}
	offset := (f.Page - 1) * f.PerPage
	totalPages := (total + f.PerPage - 1) / f.PerPage

	rows, err := db.R.QueryContext(ctx, `
		SELECT at, actor_label, action, target_kind, target_label, outcome,
		       COALESCE(detail, '{}'), COALESCE(remote_addr, '')
		FROM audit_event
		WHERE `+whereClause+`
		ORDER BY at DESC, id DESC
		LIMIT ? OFFSET ?`, append(args, f.PerPage, offset)...)
	if err != nil {
		return AuditResult{}, err
	}
	defer rows.Close()

	var events []AuditEvent
	for rows.Next() {
		var e AuditEvent
		if err := rows.Scan(&e.At, &e.ActorLabel, &e.Action, &e.TargetKind,
			&e.TargetLabel, &e.Outcome, &e.Detail, &e.SourceIP); err != nil {
			return AuditResult{}, err
		}
		events = append(events, e)
	}

	return AuditResult{
		Events:     events,
		Total:      total,
		TotalPages: totalPages,
		Page:       f.Page,
		PerPage:    f.PerPage,
	}, rows.Err()
}

// AuditActions returns the distinct action values in the audit_event table,
// which is what the Action filter select needs.
func (db *DB) AuditActions(ctx context.Context) ([]string, error) {
	rows, err := db.R.QueryContext(ctx, `SELECT DISTINCT action FROM audit_event ORDER BY action`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// auditDateSince turns the date range select into the earliest timestamp to load.
func auditDateSince(r string) string {
	var days int
	switch r {
	case "24h":
		days = 1
	case "7d":
		days = 7
	case "30d":
		days = 30
	case "90d":
		days = 90
	default:
		return "" // all time
	}
	return time.Now().AddDate(0, 0, -days).UTC().Format(time.RFC3339)
}

// CollectionStats is the aggregate figures the Collection jobs page leads with.
type CollectionStats struct {
	Runs      int
	Succeeded int
	Degraded  int
	Failed    int
	Running   int // queue figure: count where status = 'running'
	MedianMS  int64
	P95MS     int64
}

// CollectionStatsFor computes aggregates over the filtered collection set.
// The median is calculated in-process rather than in SQL because SQLite has no
// built-in percentile function, and the memory cost is acceptable — even a
// fleet with 428 nodes collecting daily is 156k rows a year, and we're reading
// a window of that.
func (db *DB) CollectionStatsFor(ctx context.Context, since string, nodeFilter, statusFilter, triggerFilter string) (CollectionStats, error) {
	where := []string{"started_at >= ?"}
	args := []any{since}

	if nodeFilter != "" {
		where = append(where, "node_id IN (SELECT id FROM node WHERE LOWER(display_name) LIKE ?)")
		args = append(args, "%"+strings.ToLower(nodeFilter)+"%")
	}
	if statusFilter != "" {
		where = append(where, "status = ?")
		args = append(args, statusFilter)
	}
	if triggerFilter != "" {
		where = append(where, "trigger = ?")
		args = append(args, triggerFilter)
	}

	whereClause := strings.Join(where, " AND ")

	var stats CollectionStats
	err := db.R.QueryRowContext(ctx, `
		SELECT
			COUNT(*) AS total,
			COALESCE(SUM(CASE WHEN status = 'succeeded' THEN 1 ELSE 0 END), 0) AS succeeded,
			COALESCE(SUM(CASE WHEN status = 'degraded' THEN 1 ELSE 0 END), 0) AS degraded,
			COALESCE(SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END), 0) AS failed
		FROM collection
		WHERE `+whereClause, args...).Scan(&stats.Runs, &stats.Succeeded, &stats.Degraded, &stats.Failed)
	if err != nil {
		return stats, err
	}

	// Queue count: running collections regardless of time window.
	if err := db.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM collection WHERE status = 'running'`).Scan(&stats.Running); err != nil {
		return stats, err
	}

	// Median and p95 duration, calculated from the set.
	rows, err := db.R.QueryContext(ctx, `
		SELECT duration_ms
		FROM collection
		WHERE `+whereClause+` AND duration_ms IS NOT NULL
		ORDER BY duration_ms`, args...)
	if err != nil {
		return stats, err
	}
	defer rows.Close()

	var durations []int64
	for rows.Next() {
		var d int64
		if err := rows.Scan(&d); err != nil {
			return stats, err
		}
		durations = append(durations, d)
	}
	if err := rows.Err(); err != nil {
		return stats, err
	}

	if len(durations) > 0 {
		sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
		stats.MedianMS = durations[len(durations)/2]
		stats.P95MS = durations[int(float64(len(durations))*0.95)]
	}

	return stats, nil
}
