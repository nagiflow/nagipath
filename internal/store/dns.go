package store

import (
	"context"
	"encoding/json"
)

// SaveDNS records what a name resolved to **on the target host**. Resolving names
// here in the control plane would answer a different question: split-horizon DNS
// is the normal case in a fleet, not an exception (ADR-0008).
func (db *DB) SaveDNS(ctx context.Context, nodeID int64, name string, addrs []string, method string) error {
	if addrs == nil {
		addrs = []string{}
	}
	blob, err := json.Marshal(addrs)
	if err != nil {
		return err
	}
	_, err = db.W.ExecContext(ctx, `INSERT OR REPLACE INTO dns_resolution
		(node_id, name, addresses, method, resolved_at) VALUES (?,?,?,?,?)`,
		nodeID, name, string(blob), method, Now())
	return err
}

// ResolvedNames returns every Node's own resolution of every name, keyed by node
// ID then name. It is deliberately per-Node rather than collapsed to one global
// answer per name: split-horizon DNS is the normal case in a fleet (see SaveDNS),
// so the same name can correctly mean different addresses on different Nodes, and
// the only resolution that is ever valid for one Hop is the one performed on the
// Node that holds the Upstream referencing that name (ADR-0010) — never another
// Node's answer, and never a merge of several Nodes' answers into one list.
// dns_resolution is keyed (node_id, name) with INSERT OR REPLACE, so there is
// exactly one row per Node per name already; no "most recent" filter is needed.
func (db *DB) ResolvedNames(ctx context.Context) (map[int64]map[string][]string, error) {
	rows, err := db.R.QueryContext(ctx,
		`SELECT node_id, name, addresses FROM dns_resolution WHERE method != 'unresolved'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]map[string][]string{}
	for rows.Next() {
		var nodeID int64
		var name, blob string
		if err := rows.Scan(&nodeID, &name, &blob); err != nil {
			return nil, err
		}
		var addrs []string
		json.Unmarshal([]byte(blob), &addrs)
		if out[nodeID] == nil {
			out[nodeID] = map[string][]string{}
		}
		out[nodeID][name] = addrs
	}
	return out, rows.Err()
}

// SetInstanceLogPaths is called after a parse, because the access log paths are a
// parser output, not a discovery output.
func (db *DB) SetInstanceLogPaths(ctx context.Context, id int64, paths []string) error {
	if paths == nil {
		paths = []string{}
	}
	blob, err := json.Marshal(paths)
	if err != nil {
		return err
	}
	_, err = db.W.ExecContext(ctx,
		`UPDATE instance SET access_log_paths = ? WHERE id = ?`, string(blob), id)
	return err
}

// SiteIDsByKey maps natural keys back to row ids for one Snapshot, so a caller
// holding a parse result can attach rows the parser could not know the ids of.
func (db *DB) SiteIDsByKey(ctx context.Context, snapshotID int64) (map[string]int64, error) {
	return db.keysToIDs(ctx, "site", snapshotID)
}

func (db *DB) ListenerIDsByKey(ctx context.Context, snapshotID int64) (map[string]int64, error) {
	return db.keysToIDs(ctx, "listener", snapshotID)
}

func (db *DB) keysToIDs(ctx context.Context, table string, snapshotID int64) (map[string]int64, error) {
	rows, err := db.R.QueryContext(ctx,
		`SELECT natural_key, id FROM `+table+` WHERE snapshot_id = ? ORDER BY ordinal`, snapshotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var k string
		var id int64
		if err := rows.Scan(&k, &id); err != nil {
			return nil, err
		}
		if _, seen := out[k]; !seen {
			out[k] = id
		}
	}
	return out, rows.Err()
}
