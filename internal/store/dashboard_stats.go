package store

import (
	"context"
	"database/sql"
)

// OldestSnapshot returns the oldest current snapshot and the node it belongs to.
// This is for the dashboard's coverage strip "oldest snapshot <age> · <node>".
func (db *DB) OldestSnapshot(ctx context.Context) (captured, nodeName string, err error) {
	err = db.R.QueryRowContext(ctx, `SELECT s.captured_at, n.display_name
		FROM snapshot s
		JOIN instance i ON i.id = s.instance_id
		JOIN node n ON n.id = i.node_id
		WHERE s.is_current = 1 AND i.retired_at IS NULL
		ORDER BY s.captured_at ASC
		LIMIT 1`).Scan(&captured, &nodeName)
	if err == sql.ErrNoRows {
		return "", "", nil
	}
	return captured, nodeName, err
}

// StaleNodeCount returns the count of nodes not collected in the last 24 hours.
func (db *DB) StaleNodeCount(ctx context.Context, cutoff string) (int, error) {
	var count int
	err := db.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM node
		WHERE retired_at IS NULL AND (last_collection IS NULL OR last_collection < ?)`, cutoff).Scan(&count)
	return count, err
}

// AvgCollectionDuration returns the average duration of collections in the last 24 hours.
func (db *DB) AvgCollectionDuration(ctx context.Context, since string) (float64, error) {
	var avg sql.NullFloat64
	err := db.R.QueryRowContext(ctx, `SELECT AVG(
		(julianday(finished_at) - julianday(started_at)) * 86400
	) FROM collection
	WHERE started_at >= ? AND finished_at IS NOT NULL AND finished_at > started_at`,
		since).Scan(&avg)
	if !avg.Valid {
		return 0, err
	}
	return avg.Float64, err
}
