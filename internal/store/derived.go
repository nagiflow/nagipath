package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/nagiflow/nagipath/internal/parse"
)

// SaveDerived writes one parse result as derived rows, in one transaction, and
// stamps every row with the current ParserVersion. A half-written topology is
// worse than none, so it is all or nothing.
//
// Insert order matters: Listeners and Upstreams first (Sites and Routes reference
// them), then Sites, then Routes, then Rules — Rules carry a scope_id pointing at
// whichever of those owns them.
func (db *DB) SaveDerived(ctx context.Context, snapshotID, instanceID int64, res *parse.Result) error {
	files, err := db.SnapshotFiles(ctx, snapshotID)
	if err != nil {
		return err
	}
	fileID := map[string]int64{}
	var fallback int64
	for _, f := range files {
		if f.Kind == "config_file" || f.Kind == "vendor_dump" {
			if fallback == 0 {
				fallback = f.ID
			}
		}
		fileID[f.Path] = f.ID
	}
	if fallback == 0 {
		return fmt.Errorf("snapshot %d has no config files to anchor provenance to", snapshotID)
	}
	prov := func(p parse.Prov) (int64, int, int) {
		if id, ok := fileID[p.Path]; ok {
			return id, p.Start, p.End
		}
		// Provenance we cannot anchor points at the vendor dump with a zero range
		// rather than being silently dropped: a row with weak provenance is still
		// a row, and the UI says so.
		return fallback, 0, 0
	}

	tx, err := db.W.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Re-deriving a Snapshot replaces its rows wholesale (ADR-0012: derived data
	// is recomputed, never migrated in place).
	for _, t := range []string{"rule", "upstream_member", "route", "site", "upstream", "listener"} {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM `+t+` WHERE snapshot_id = ?`, snapshotID); err != nil {
			return err
		}
	}

	pv := ParserVersion
	listenerID := map[string]int64{}
	for _, l := range res.Listeners {
		f, s, e := prov(l.Prov)
		r, err := tx.ExecContext(ctx, `INSERT INTO listener
			(snapshot_id, instance_id, parser_version, natural_key, ordinal,
			 prov_file_id, prov_byte_start, prov_byte_end,
			 address, port, tls, protocol, is_default, raw_text)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			snapshotID, instanceID, pv, l.NaturalKey, l.Ordinal, f, s, e,
			l.Address, l.Port, boolInt(l.TLS), l.Protocol, boolInt(l.IsDefault), l.Raw)
		if err != nil {
			return fmt.Errorf("listener %s: %w", l.NaturalKey, err)
		}
		if listenerID[l.NaturalKey], err = r.LastInsertId(); err != nil {
			return err
		}
	}

	upstreamID := map[string]int64{}
	for _, up := range res.Upstreams {
		f, s, e := prov(up.Prov)
		r, err := tx.ExecContext(ctx, `INSERT INTO upstream
			(snapshot_id, instance_id, parser_version, natural_key, ordinal,
			 prov_file_id, prov_byte_start, prov_byte_end,
			 name, kind, balance_method, raw_text)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
			snapshotID, instanceID, pv, up.NaturalKey, up.Ordinal, f, s, e,
			up.Name, up.Kind, up.BalanceMethod, up.Raw)
		if err != nil {
			return fmt.Errorf("upstream %s: %w", up.NaturalKey, err)
		}
		id, err := r.LastInsertId()
		if err != nil {
			return err
		}
		upstreamID[up.NaturalKey] = id
		for _, m := range up.Members {
			mf, ms, me := prov(m.Prov)
			if _, err := tx.ExecContext(ctx, `INSERT INTO upstream_member
				(snapshot_id, instance_id, parser_version, natural_key, ordinal,
				 prov_file_id, prov_byte_start, prov_byte_end,
				 upstream_id, host, port, scheme, weight, flags, raw_text)
				VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				snapshotID, instanceID, pv, m.NaturalKey, m.Ordinal, mf, ms, me,
				id, m.Host, nullZero(m.Port), m.Scheme, nullZero(m.Weight), m.Flags, m.Raw); err != nil {
				return fmt.Errorf("member %s: %w", m.NaturalKey, err)
			}
		}
	}

	insertRule := func(scope string, scopeID *int64, ru *parse.Rule) error {
		f, s, e := prov(ru.Prov)
		r, err := tx.ExecContext(ctx, `INSERT INTO rule
			(snapshot_id, instance_id, parser_version, natural_key, ordinal,
			 prov_file_id, prov_byte_start, prov_byte_end,
			 scope_kind, scope_id, directive, action_class, args, raw_text,
			 is_modelled, shadowed, shadowed_by)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			snapshotID, instanceID, pv, ru.NaturalKey, ru.Ordinal, f, s, e,
			scope, scopeID, ru.Directive, ru.ActionClass, ru.Args, ru.Raw,
			boolInt(ru.IsModelled), boolInt(ru.Shadowed), ru.ShadowedBy)
		if err != nil {
			return fmt.Errorf("rule %s: %w", ru.NaturalKey, err)
		}
		_, err = r.LastInsertId()
		return err
	}

	for _, ru := range res.GlobalRules {
		if err := insertRule(ru.ScopeKind, nil, ru); err != nil {
			return err
		}
	}

	var insertRoute func(siteID int64, parentID *int64, rt *parse.Route) error
	insertRoute = func(siteID int64, parentID *int64, rt *parse.Route) error {
		f, s, e := prov(rt.Prov)
		var upID any
		if id, ok := upstreamID[rt.UpstreamKey]; ok {
			upID = id
		}
		r, err := tx.ExecContext(ctx, `INSERT INTO route
			(snapshot_id, instance_id, parser_version, natural_key, ordinal,
			 prov_file_id, prov_byte_start, prov_byte_end,
			 site_id, parent_route_id, match_type, pattern, precedence_rank,
			 specificity, upstream_id, target_raw, is_terminal, raw_text)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			snapshotID, instanceID, pv, rt.NaturalKey, rt.Ordinal, f, s, e,
			siteID, parentID, rt.MatchType, rt.Pattern, rt.PrecedenceRank,
			rt.Specificity, upID, rt.TargetRaw, boolInt(rt.IsTerminal), rt.Raw)
		if err != nil {
			return fmt.Errorf("route %s: %w", rt.NaturalKey, err)
		}
		id, err := r.LastInsertId()
		if err != nil {
			return err
		}
		for _, ru := range rt.Rules {
			if err := insertRule("route", &id, ru); err != nil {
				return err
			}
		}
		for _, child := range rt.Routes {
			if err := insertRoute(siteID, &id, child); err != nil {
				return err
			}
		}
		return nil
	}

	for _, site := range res.Sites {
		ids := make([]int64, 0, len(site.ListenerKeys))
		for _, k := range site.ListenerKeys {
			if id, ok := listenerID[k]; ok {
				ids = append(ids, id)
			}
		}
		listeners, err := json.Marshal(ids)
		if err != nil {
			return err
		}
		f, s, e := prov(site.Prov)
		r, err := tx.ExecContext(ctx, `INSERT INTO site
			(snapshot_id, instance_id, parser_version, natural_key, ordinal,
			 prov_file_id, prov_byte_start, prov_byte_end,
			 listener_ids, primary_name, kind, document_root, raw_text)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			snapshotID, instanceID, pv, site.NaturalKey, site.Ordinal, f, s, e,
			string(listeners), site.PrimaryName, site.Kind, site.DocumentRoot, site.Raw)
		if err != nil {
			return fmt.Errorf("site %s: %w", site.NaturalKey, err)
		}
		siteID, err := r.LastInsertId()
		if err != nil {
			return err
		}
		for _, n := range site.Names {
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO site_name
				(site_id, name, match_kind, ordinal) VALUES (?,?,?,?)`,
				siteID, n.Name, n.MatchKind, n.Ordinal); err != nil {
				return fmt.Errorf("site_name %s: %w", n.Name, err)
			}
		}
		for _, ru := range site.Rules {
			if err := insertRule("site", &siteID, ru); err != nil {
				return err
			}
		}
		for _, rt := range site.Routes {
			if err := insertRoute(siteID, nil, rt); err != nil {
				return err
			}
		}
	}

	// rule_fts is an external-content FTS5 index, so it is not maintained by
	// triggers and does not follow rule deletions.
	// ponytail: rebuilt per snapshot here, which is O(all rules) per collection.
	// Swap for incremental delete/insert if collection latency starts to hurt.
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO rule_fts(rule_fts) VALUES('rebuild')`); err != nil {
		return fmt.Errorf("rule_fts rebuild: %w", err)
	}

	// Degradation accumulates. The capture already recorded whether the file set
	// could be proven complete; a clean parse of an incomplete capture is still an
	// incomplete answer, so this ORs in rather than overwriting.
	if _, err := tx.ExecContext(ctx,
		`UPDATE snapshot SET parse_state = ?, parse_error = ?, parser_version = ?,
		 degraded = MAX(degraded, ?),
		 degraded_reason = TRIM(degraded_reason || CASE
		   WHEN ? = '' OR instr(degraded_reason, ?) > 0 THEN ''
		   WHEN degraded_reason = '' THEN ? ELSE '; ' || ? END)
		 WHERE id = ?`,
		"parsed", "", pv, boolInt(res.Degraded),
		res.DegradedReason, res.DegradedReason, res.DegradedReason, res.DegradedReason,
		snapshotID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return db.reindexSnapshotText(ctx, instanceID, snapshotID)
}

// reindexSnapshotText keeps the full-text index scoped to **current** Snapshots
// only. Indexing all history would store an uncompressed copy of every file ever
// collected, which defeats content-addressed storage (PRD-V1 §3.2).
func (db *DB) reindexSnapshotText(ctx context.Context, instanceID, snapshotID int64) error {
	rows, err := db.R.QueryContext(ctx, `SELECT sf.id FROM snapshot_file sf
		JOIN snapshot s ON s.id = sf.snapshot_id
		WHERE s.instance_id = ? AND s.id != ?`, instanceID, snapshotID)
	if err != nil {
		return err
	}
	var stale []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		stale = append(stale, id)
	}
	rows.Close()

	tx, err := db.W.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range stale {
		var rowid int64
		err := tx.QueryRowContext(ctx,
			`SELECT rowid FROM snapshot_text_fts_map WHERE snapshot_file_id = ?`, id).Scan(&rowid)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM snapshot_text_fts WHERE rowid = ?`, rowid); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM snapshot_text_fts_map WHERE rowid = ?`, rowid); err != nil {
			return err
		}
	}

	files, err := db.SnapshotFiles(ctx, snapshotID)
	if err != nil {
		return err
	}
	// Config files are the index. The vendor dump is indexed only when there are no
	// config files at all: it is the same directives again, so indexing both would
	// return every hit twice — but an instance nagipath could only read through
	// `nginx -T` would otherwise have no text index, and the search page tells the
	// operator a miss means "the words are genuinely absent".
	kind := "config_file"
	if !hasKind(files, kind) {
		kind = "vendor_dump"
	}
	for _, f := range files {
		if f.Kind != kind {
			continue
		}
		body, err := db.Blob(ctx, f.Digest)
		if err != nil {
			return err
		}
		r, err := tx.ExecContext(ctx,
			`INSERT INTO snapshot_text_fts (path, body) VALUES (?,?)`, f.Path, string(body))
		if err != nil {
			return err
		}
		rowid, err := r.LastInsertId()
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO snapshot_text_fts_map (rowid, snapshot_file_id) VALUES (?,?)`,
			rowid, f.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func hasKind(files []FileRef, kind string) bool {
	for _, f := range files {
		if f.Kind == kind {
			return true
		}
	}
	return false
}

func nullZero(n int) any {
	if n == 0 {
		return nil
	}
	return n
}
