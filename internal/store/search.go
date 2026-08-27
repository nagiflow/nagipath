package store

import (
	"context"
	"strings"
)

// RuleHit is one directive found by search, with enough context to open it.
type RuleHit struct {
	RuleID      int64
	InstanceID  int64
	SnapshotID  int64
	FileID      int64
	Instance    string
	Vendor      string
	Node        string
	Cluster     string
	Directive   string
	ActionClass string
	Args        string
	Raw         string
	Path        string
	ByteStart   int
	Shadowed    bool
}

// TextHit is one configuration file whose text matched, with a snippet.
type TextHit struct {
	FileID     int64
	SnapshotID int64
	InstanceID int64
	Instance   string
	Vendor     string
	// Node, because an instance display name is vendor + config basename: five
	// nodes running stock nginx all read "nginx nginx.conf", and a result list of
	// five identical rows cannot be acted on. RuleHit already carried this.
	Node    string
	Cluster string
	Path    string
	Snippet string
}

// SearchRules is the structured search: it answers "which directives across the
// fleet mention this?" and is scoped to current Snapshots, because a hit in a
// retired Snapshot is history, not a finding.
func (db *DB) SearchRules(ctx context.Context, query string, limit int) ([]RuleHit, error) {
	return db.SearchRulesScoped(ctx, query, limit, true)
}

// SearchRulesScoped searches rules with optional scope restriction.
func (db *DB) SearchRulesScoped(ctx context.Context, query string, limit int, currentOnly bool) ([]RuleHit, error) {
	scopeFilter := ""
	if currentOnly {
		scopeFilter = "AND s.is_current = 1"
	}
	sql := `SELECT r.id, r.instance_id, r.snapshot_id, f.id,
		i.display_name, i.vendor, n.display_name, COALESCE(cl.name, ''),
		r.directive, r.action_class, r.args,
		r.raw_text, f.path, r.prov_byte_start, r.shadowed
		FROM rule_fts
		JOIN rule r ON r.id = rule_fts.rowid
		JOIN snapshot s ON s.id = r.snapshot_id ` + scopeFilter + `
		JOIN instance i ON i.id = r.instance_id
		JOIN node n ON n.id = i.node_id
		LEFT JOIN cluster cl ON cl.id = i.cluster_id
		JOIN snapshot_file f ON f.id = r.prov_file_id
		WHERE rule_fts MATCH ?
		ORDER BY rank LIMIT ?`
	rows, err := db.R.QueryContext(ctx, sql, ftsQuery(query), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RuleHit
	for rows.Next() {
		var h RuleHit
		if err := rows.Scan(&h.RuleID, &h.InstanceID, &h.SnapshotID, &h.FileID, &h.Instance,
			&h.Vendor, &h.Node, &h.Cluster, &h.Directive, &h.ActionClass, &h.Args, &h.Raw,
			&h.Path, &h.ByteStart, &h.Shadowed); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// SearchConfigText is the escape hatch for everything the parser does not model:
// verbatim text of the current Snapshots' configuration files.
func (db *DB) SearchConfigText(ctx context.Context, query string, limit int) ([]TextHit, error) {
	return db.SearchConfigTextScoped(ctx, query, limit, true)
}

// SearchConfigTextScoped searches config text with optional scope restriction.
func (db *DB) SearchConfigTextScoped(ctx context.Context, query string, limit int, currentOnly bool) ([]TextHit, error) {
	scopeFilter := ""
	if currentOnly {
		scopeFilter = "AND s.is_current = 1"
	}
	sql := `SELECT f.id, f.snapshot_id, s.instance_id,
		i.display_name, i.vendor, n.display_name, COALESCE(cl.name, ''), f.path,
		snippet(snapshot_text_fts, 1, '[', ']', '…', 12)
		FROM snapshot_text_fts
		JOIN snapshot_text_fts_map mp ON mp.rowid = snapshot_text_fts.rowid
		JOIN snapshot_file f ON f.id = mp.snapshot_file_id
		JOIN snapshot s ON s.id = f.snapshot_id ` + scopeFilter + `
		JOIN instance i ON i.id = s.instance_id
		JOIN node n ON n.id = i.node_id
		LEFT JOIN cluster cl ON cl.id = i.cluster_id
		WHERE snapshot_text_fts MATCH ?
		ORDER BY rank LIMIT ?`
	rows, err := db.R.QueryContext(ctx, sql, ftsQuery(query), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TextHit
	for rows.Next() {
		var h TextHit
		if err := rows.Scan(&h.FileID, &h.SnapshotID, &h.InstanceID, &h.Instance,
			&h.Vendor, &h.Node, &h.Cluster, &h.Path, &h.Snippet); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// ftsQuery quotes each term so operator input cannot be an FTS5 syntax error.
// Operators type `proxy_pass`, `X-Frame-Options` and `10.90.4.*` — all of which
// contain FTS5 operators — and a syntax error in response to a plausible search
// is indistinguishable from "no results".
func ftsQuery(q string) string {
	fields := strings.Fields(q)
	quoted := make([]string, 0, len(fields))
	for _, f := range fields {
		prefix := ""
		if strings.HasSuffix(f, "*") {
			f, prefix = strings.TrimSuffix(f, "*"), "*"
		}
		f = strings.ReplaceAll(f, `"`, `""`)
		if f == "" {
			continue
		}
		quoted = append(quoted, `"`+f+`"`+prefix)
	}
	if len(quoted) == 0 {
		return `""`
	}
	return strings.Join(quoted, " ")
}
