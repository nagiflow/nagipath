package store

import "context"

// RuleCount is how many rules the fleet's current configuration adds up to, for
// the dashboard's "Indexed rules" tile.
//
// is_current is the canonical marker of "the configuration in force" —
// WriteSnapshot maintains exactly one per instance inside the transaction that
// stores it. Re-deriving it here as MAX(id) per instance would be a second
// opinion that can disagree with the first.
func (db *DB) RuleCount(ctx context.Context) (int, error) {
	var count int
	err := db.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM rule r
		JOIN snapshot s ON s.id = r.snapshot_id AND s.is_current = 1
		JOIN instance i ON i.id = s.instance_id AND i.retired_at IS NULL`).Scan(&count)
	return count, err
}
