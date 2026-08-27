// Package store owns the SQLite connection, the embedded migrations and the
// typed query layer. Two pools: one writer (MaxOpenConns 1, so SQLITE_BUSY is
// structurally impossible on writes) and one reader.
//
// ponytail: schema.md says "sqlc-generated typed queries only". This is
// hand-written typed queries over database/sql with SQL in named constants —
// same shape, no codegen step in the build. Swap in sqlc when the query count
// makes generation cheaper than maintenance.
package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// ParserVersion is bumped whenever parsing behaviour changes. Derived rows
// carry it, and anything computed at an older version is recomputed rather
// than displayed (ADR-0012).
const ParserVersion = 1

type DB struct {
	W    *sql.DB // writer: exactly one connection
	R    *sql.DB // readers
	Path string
}

// Open connects both pools, applies pragmas and runs migrations forward.
func Open(path string) (*DB, error) {
	if err := ensureDir(path); err != nil {
		return nil, err
	}
	w, err := open(path, 1)
	if err != nil {
		return nil, err
	}
	r, err := open(path, max(2, runtime.NumCPU()))
	if err != nil {
		w.Close()
		return nil, err
	}
	db := &DB{W: w, R: r, Path: path}
	if err := db.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func open(path string, maxConns int) (*sql.DB, error) {
	dsn := path + "?_pragma=busy_timeout(10000)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_pragma=foreign_keys(ON)"
	d, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	d.SetMaxOpenConns(maxConns)
	d.SetMaxIdleConns(maxConns)
	d.SetConnMaxLifetime(0)
	if err := d.Ping(); err != nil {
		d.Close()
		return nil, fmt.Errorf("ping %s: %w", path, err)
	}
	return d, nil
}

func (db *DB) Close() error {
	if db.R != nil {
		db.R.Close()
	}
	if db.W != nil {
		return db.W.Close()
	}
	return nil
}

// ---------------------------------------------------------------- migrations

// migrate applies every unapplied migration, each in its own transaction, in
// version order. Forward-only: there are no down migrations, and a failure
// leaves the database at the last good version rather than half-applied.
func (db *DB) migrate() error {
	ctx := context.Background()
	if _, err := db.W.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migration (
		version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL) STRICT`); err != nil {
		return fmt.Errorf("migration table: %w", err)
	}

	applied := map[int]bool{}
	rows, err := db.W.QueryContext(ctx, `SELECT version FROM schema_migration`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return err
		}
		applied[v] = true
	}
	rows.Close()

	names, err := migrationNames()
	if err != nil {
		return err
	}

	for _, name := range names {
		version, err := strconv.Atoi(strings.SplitN(name, "_", 2)[0])
		if err != nil {
			return fmt.Errorf("migration %s: filename must start with a version number", name)
		}
		if applied[version] {
			continue
		}
		body, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		tx, err := db.W.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s failed: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migration (version, name, applied_at) VALUES (?,?,?)`,
			version, name, Now()); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("migration %s commit: %w", name, err)
		}
	}
	return nil
}

// migrationNames lists every embedded migration file, in version order. Shared
// by migrate() (applying them) and ExpectedMigrationCount (/readyz's check that
// nothing is left unapplied).
func migrationNames() ([]string, error) {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names, nil
}

// ExpectedMigrationCount is how many migrations this binary embeds, for
// /readyz to compare against AppliedMigrations.
func ExpectedMigrationCount() (int, error) {
	names, err := migrationNames()
	if err != nil {
		return 0, err
	}
	return len(names), nil
}

// Ping is a trivial reachability check against the reader pool, for /readyz.
func (db *DB) Ping(ctx context.Context) error {
	var n int
	return db.R.QueryRowContext(ctx, `SELECT 1`).Scan(&n)
}

// AppliedMigrations reports what has run, for the diagnostics page.
func (db *DB) AppliedMigrations(ctx context.Context) ([]string, error) {
	rows, err := db.R.QueryContext(ctx,
		`SELECT name FROM schema_migration ORDER BY version`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// ------------------------------------------------------------------ settings

// Defaults for every setting key, so an empty table is a working system
// (schema.md §3).
var settingDefaults = map[string]string{
	"collection_interval_seconds":         "3600",
	"collection_jitter_seconds":           "300",
	"snapshot_retention_days":             "90",
	"snapshot_retention_min_per_instance": "10",
	// The other three retention windows, in the same table and editable from
	// Settings · Retention. 0 in any of them means keep forever — see Prune.
	"job_log_retention_days":          "30",
	"probe_retention_days":            "90",
	"audit_retention_days":            "400",
	"probe_max_redirects":             "5",
	"probe_log_lookback_seconds":      "120",
	"probe_rate_limit_max":            "5",
	"probe_rate_limit_window_seconds": "300",
	"login_rate_limit_max":            "5",
	"login_rate_limit_window_seconds": "300",
	"ssh_command_timeout_seconds":     "30",
	"readfile_max_bytes":              "8388608",
	"snapshot_max_files":              "2000",
	"quarantine_after_failures":       "10",
	"ssh_workers":                     "8",
}

func (db *DB) Setting(ctx context.Context, key string) string {
	var v string
	err := db.R.QueryRowContext(ctx, `SELECT value FROM setting WHERE key = ?`, key).Scan(&v)
	if err == nil {
		return v
	}
	return settingDefaults[key]
}

func (db *DB) SettingInt(ctx context.Context, key string) int {
	n, err := strconv.Atoi(db.Setting(ctx, key))
	if err != nil {
		n, _ = strconv.Atoi(settingDefaults[key])
	}
	return n
}

func (db *DB) SetSetting(ctx context.Context, key, value string, by *int64) error {
	_, err := db.W.ExecContext(ctx, `INSERT INTO setting (key, value, updated_at, updated_by)
		VALUES (?,?,?,?) ON CONFLICT(key) DO UPDATE SET
		value = excluded.value, updated_at = excluded.updated_at, updated_by = excluded.updated_by`,
		key, value, Now(), by)
	return err
}

// -------------------------------------------------------------------- helpers

// Now is the single source of timestamps: ISO-8601 UTC with a Z suffix, so
// lexical sort equals chronological sort (schema.md §1).
func Now() string { return time.Now().UTC().Format("2006-01-02T15:04:05Z") }

func ensureDir(path string) error {
	dir := filepath.Dir(path)
	if dir == "" || dir == "." {
		return nil
	}
	return mkdirAll(dir)
}

// NullString is a small convenience for the many nullable TEXT columns.
func NullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
