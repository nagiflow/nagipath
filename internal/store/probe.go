package store

import "context"

// ProbeCount is the total number of Probes ever sent, for /metrics.
func (db *DB) ProbeCount(ctx context.Context) (int, error) {
	var n int
	err := db.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM probe`).Scan(&n)
	return n, err
}
