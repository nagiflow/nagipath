package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"strconv"
)

// SettingsViewData aggregates counts and summaries for the Settings index.
type SettingsViewData struct {
	Users                      []User
	SnapshotDays, JobLogDays   int
	ProbeDays, AuditDays       int
	SSHWorkers, CommandTimeout int
	CollectionIntervalMinutes  int
	DefaultCredential          int64
	DefaultCredentialName      string
}

// SettingsView loads the data for the Settings index landing page.
func (db *DB) SettingsView(ctx context.Context) (SettingsViewData, error) {
	users, err := db.Users(ctx)
	if err != nil {
		return SettingsViewData{}, err
	}
	defaultCredID, _ := parseInt64(db.Setting(ctx, "default_credential_id"))
	var credName string
	if defaultCredID > 0 {
		_ = db.R.QueryRowContext(ctx, `SELECT name FROM credential WHERE id = ?`, defaultCredID).Scan(&credName)
	}
	return SettingsViewData{
		Users:                     users,
		SnapshotDays:              db.SettingInt(ctx, "snapshot_retention_days"),
		JobLogDays:                db.SettingInt(ctx, "job_log_retention_days"),
		ProbeDays:                 db.SettingInt(ctx, "probe_retention_days"),
		AuditDays:                 db.SettingInt(ctx, "audit_retention_days"),
		SSHWorkers:                db.SettingInt(ctx, "ssh_workers"),
		CommandTimeout:            db.SettingInt(ctx, "ssh_command_timeout_seconds"),
		CollectionIntervalMinutes: db.SettingInt(ctx, "collection_interval_seconds") / 60,
		DefaultCredential:         defaultCredID,
		DefaultCredentialName:     credName,
	}, nil
}

// HostKeyStats summarizes the state of host keys across the fleet.
type HostKeyStats struct {
	Pending    int
	Changed    int
	Approved   int
	Algorithms map[string]int
}

func (db *DB) HostKeyStats(ctx context.Context) (HostKeyStats, error) {
	var s HostKeyStats
	s.Algorithms = make(map[string]int)

	_ = db.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM host_key WHERE state = 'pending'`).Scan(&s.Pending)
	// Changed means a pending key exists and an approved key for that algorithm already exists
	_ = db.R.QueryRowContext(ctx, `SELECT COUNT(DISTINCT h.node_id) FROM host_key h
		WHERE h.state = 'pending' AND EXISTS (
			SELECT 1 FROM host_key p WHERE p.node_id = h.node_id
			AND p.algorithm = h.algorithm AND p.state = 'approved')`).Scan(&s.Changed)
	_ = db.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM host_key WHERE state = 'approved'`).Scan(&s.Approved)

	rows, err := db.R.QueryContext(ctx, `SELECT algorithm, COUNT(*) FROM host_key
		WHERE state IN ('approved', 'pending') GROUP BY algorithm`)
	if err != nil {
		return s, err
	}
	defer rows.Close()
	for rows.Next() {
		var alg string
		var n int
		if err := rows.Scan(&alg, &n); err == nil {
			s.Algorithms[alg] = n
		}
	}
	return s, nil
}

// AllHostKeys returns host keys across all states, with optional filters.
func (db *DB) AllHostKeys(ctx context.Context, stateFilter, clusterFilter string) ([]PendingHostKey, error) {
	q := `SELECT h.id, h.node_id, h.algorithm, h.public_key, h.fingerprint, h.state,
		h.first_seen_at, h.decided_at, n.display_name, n.address,
		-- A node has no cluster of its own; its processes do, and they can sit in
		-- more than one. Naming a single cluster here would invent a fact, so a
		-- node spanning two reads "web, edge" rather than picking one.
		(SELECT group_concat(DISTINCT c.name) FROM instance i
		   JOIN cluster c ON c.id = i.cluster_id WHERE i.node_id = n.id),
		COALESCE((SELECT p.fingerprint FROM host_key p WHERE p.node_id = h.node_id
		   AND p.algorithm = h.algorithm AND p.state = 'approved' AND p.id != h.id LIMIT 1), '')
		FROM host_key h
		JOIN node n ON n.id = h.node_id
		WHERE 1=1`
	args := []any{}

	if stateFilter != "" {
		switch stateFilter {
		case "pending":
			q += ` AND h.state = 'pending'`
		case "approved":
			q += ` AND h.state = 'approved'`
		case "changed":
			q += ` AND h.state = 'pending' AND EXISTS (
				SELECT 1 FROM host_key p WHERE p.node_id = h.node_id
				AND p.algorithm = h.algorithm AND p.state = 'approved')`
		}
	}
	if clusterFilter != "" {
		q += ` AND c.name = ?`
		args = append(args, clusterFilter)
	}
	q += ` ORDER BY CASE h.state WHEN 'pending' THEN 0 ELSE 1 END, n.display_name, h.algorithm`

	rows, err := db.R.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PendingHostKey
	for rows.Next() {
		var p PendingHostKey
		var clusterName sql.NullString
		if err := rows.Scan(&p.ID, &p.NodeID, &p.Algorithm, &p.PublicKey, &p.Fingerprint,
			&p.State, &p.FirstSeenAt, &p.DecidedAt, &p.NodeName, &p.NodeAddress,
			&clusterName, &p.Previous); err != nil {
			return nil, err
		}
		p.Cluster = clusterName.String
		out = append(out, p)
	}
	return out, rows.Err()
}

// MasterKeyEncryptedCounts returns the number of items encrypted by the master key.
type MasterKeyEncryptedCounts struct {
	Credentials int
	APIKeys     int
	// JobLogs counts collection rows that recorded credentials (had SSH output)
	JobLogs int
}

func (db *DB) MasterKeyEncryptedCounts(ctx context.Context) (MasterKeyEncryptedCounts, error) {
	var c MasterKeyEncryptedCounts
	_ = db.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM credential`).Scan(&c.Credentials)
	_ = db.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM api_token WHERE revoked_at IS NULL`).Scan(&c.APIKeys)
	_ = db.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM collection WHERE status = 'succeeded'`).Scan(&c.JobLogs)
	return c, nil
}

// RetentionStats summarizes the data retention state.
type RetentionStats struct {
	IndexSizeBytes int64
	Snapshots      int
	JobLogs        int
	Traces         int
	LastPrune      *PruneHistory
}

func (db *DB) RetentionStats(ctx context.Context) (RetentionStats, error) {
	var s RetentionStats

	// Stat the SQLite file for its size on disk
	if info, err := os.Stat(db.Path); err == nil {
		s.IndexSizeBytes = info.Size()
	}

	_ = db.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM snapshot`).Scan(&s.Snapshots)
	_ = db.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM collection`).Scan(&s.JobLogs)
	_ = db.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM probe WHERE result <> 'running'`).Scan(&s.Traces)

	// Last prune summary from settings store
	if pruneJSON := db.Setting(ctx, "last_prune_summary"); pruneJSON != "" {
		var ph PruneHistory
		if err := json.Unmarshal([]byte(pruneJSON), &ph); err == nil {
			s.LastPrune = &ph
		}
	}
	return s, nil
}

// PruneHistory is one prune run's summary, persisted to the settings store.
type PruneHistory struct {
	RanAt    string        `json:"ran_at"`
	Duration float64       `json:"duration_seconds"`
	Examined PruneExamined `json:"examined"`
	Deleted  Pruned        `json:"deleted"`
	Kept     PruneKept     `json:"kept"`
	Freed    int64         `json:"freed_bytes"`
}

type PruneExamined struct {
	Snapshots   int `json:"snapshots"`
	JobLogs     int `json:"job_logs"`
	Traces      int `json:"traces"`
	AuditEvents int `json:"audit_events"`
}

type PruneKept struct {
	Snapshots   int `json:"snapshots"`
	JobLogs     int `json:"job_logs"`
	Traces      int `json:"traces"`
	AuditEvents int `json:"audit_events"`
}

// SavePruneHistory persists a prune run's summary to the settings store.
func (db *DB) SavePruneHistory(ctx context.Context, ph PruneHistory, by *int64) error {
	data, err := json.Marshal(ph)
	if err != nil {
		return err
	}
	return db.SetSetting(ctx, "last_prune_summary", string(data), by)
}

// APITokensFiltered returns API tokens with optional state filtering.
func (db *DB) APITokensFiltered(ctx context.Context, stateFilter string) ([]APIToken, error) {
	q := `SELECT t.id, t.name, t.user_id, u.username, t.created_at, t.last_used_at,
		t.expires_at, t.revoked_at, COALESCE(t.prefix, '') as prefix
		FROM api_token t JOIN app_user u ON u.id = t.user_id WHERE 1=1`
	args := []any{}

	switch stateFilter {
	case "active":
		q += ` AND t.revoked_at IS NULL AND (t.expires_at IS NULL OR t.expires_at > ?)`
		args = append(args, Now())
	case "revoked":
		q += ` AND t.revoked_at IS NOT NULL`
	case "expired":
		q += ` AND t.revoked_at IS NULL AND t.expires_at IS NOT NULL AND t.expires_at <= ?`
		args = append(args, Now())
	}
	q += ` ORDER BY t.created_at DESC, t.id DESC`

	rows, err := db.R.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APIToken
	for rows.Next() {
		var t APIToken
		if err := rows.Scan(&t.ID, &t.Name, &t.UserID, &t.Username, &t.CreatedAt,
			&t.LastUsedAt, &t.ExpiresAt, &t.RevokedAt, &t.Prefix); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func parseInt64(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	return strconv.ParseInt(s, 10, 64)
}
