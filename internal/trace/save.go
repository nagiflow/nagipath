package trace

import (
	"context"
	"encoding/json"

	"github.com/nagiflow/nagipath/internal/store"
)

// Save persists a computed Trace so a Probe can later attach evidence to its
// Hops and so an operator can link to the exact answer they saw.
//
// Entry candidates, undetermined branches and per-Hop path changes are not
// stored: they are re-derived from the same Snapshots by Walk, and duplicating
// them in tables would let the two drift. The Trace row is the anchor; the
// explanation is recomputed.
func Save(ctx context.Context, db *store.DB, tr *Trace, entryPointID *int64) (int64, error) {
	snapshots, err := json.Marshal(tr.SnapshotSet)
	if err != nil {
		return 0, err
	}
	if tr.SnapshotSet == nil {
		snapshots = []byte("[]")
	}

	tx, err := db.W.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var scheme, hostname, path any
	var port any
	if entryPointID == nil {
		scheme, hostname, path, port = tr.Query.Scheme, tr.Query.Hostname, tr.Query.Path, tr.Query.Port
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO trace
		(entry_point_id, ad_hoc_scheme, ad_hoc_hostname, ad_hoc_path, ad_hoc_port,
		 computed_at, parser_version, snapshot_set, hop_count, confidence, terminal_reason)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		entryPointID, scheme, hostname, path, port,
		store.Now(), store.ParserVersion, string(snapshots), len(tr.Hops),
		tr.Confidence, tr.TerminalReason)
	if err != nil {
		return 0, err
	}
	traceID, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}

	for _, h := range tr.Hops {
		var instID, snapID, listenerID, siteID, routeID, upstreamID any
		if h.Inst != nil {
			instID, snapID = h.Inst.ID, h.Inst.SnapshotID
		}
		if h.Listener != nil {
			listenerID = h.Listener.ID
		}
		if h.Site != nil {
			siteID = h.Site.ID
		}
		if h.Route != nil {
			routeID = h.Route.ID
		}
		if h.Upstream != nil {
			upstreamID = h.Upstream.ID
		}
		hr, err := tx.ExecContext(ctx, `INSERT INTO hop
			(trace_id, ordinal, is_external, instance_id, snapshot_id, listener_id, site_id,
			 route_id, upstream_id, external_target, external_reason, inbound_path,
			 effective_path, confidence)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			traceID, h.Ordinal, boolInt(h.IsExternal), instID, snapID, listenerID, siteID,
			routeID, upstreamID, h.ExternalTarget, h.ExternalReason, h.InboundPath,
			h.EffectivePath, hopConfidence(h.Confidence))
		if err != nil {
			return 0, err
		}
		hopID, err := hr.LastInsertId()
		if err != nil {
			return 0, err
		}

		// Shadowed Rules are recorded alongside applied ones. A header that is
		// silently discarded is exactly what an operator came here to find, so it
		// has to survive into storage rather than only into the rendered page.
		ordinal := 0
		for _, set := range [][]HopRule{h.Rules, h.Shadowed} {
			for _, rule := range set {
				var inherited any
				if rule.Scope == "route" && h.Route != nil && h.Route.ParentID.Valid {
					inherited = h.Route.ParentID.Int64
				}
				if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO hop_rule
					(hop_id, rule_id, ordinal, inherited_from_route_id, shadowed, confidence)
					VALUES (?,?,?,?,?,?)`,
					hopID, rule.Rule.ID, ordinal, inherited,
					boolInt(rule.Rule.Shadowed), rule.Confidence); err != nil {
					return 0, err
				}
				ordinal++
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	tr.ID = traceID
	return traceID, nil
}

// Recent lists stored Traces for the history view.
type Summary struct {
	ID             int64
	Scheme         string
	Hostname       string
	Path           string
	Port           int
	ComputedAt     string
	HopCount       int
	Confidence     string
	TerminalReason string
}

func Recent(ctx context.Context, db *store.DB, limit int) ([]Summary, error) {
	rows, err := db.R.QueryContext(ctx, `SELECT id,
		COALESCE(ad_hoc_scheme,''), COALESCE(ad_hoc_hostname,''), COALESCE(ad_hoc_path,''),
		COALESCE(ad_hoc_port,0), computed_at, hop_count, confidence, terminal_reason
		FROM trace ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Summary
	for rows.Next() {
		var s Summary
		if err := rows.Scan(&s.ID, &s.Scheme, &s.Hostname, &s.Path, &s.Port,
			&s.ComputedAt, &s.HopCount, &s.Confidence, &s.TerminalReason); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// hopConfidence maps a Hop's displayed confidence onto the stored ladder. A Hop
// that rests on an incomplete Snapshot reads as "partial" on the page, but the
// stored column is the evidence ladder a Probe climbs — inferred, then
// observed_effect, then verified — and "partial" is not a rung on it. Stored as
// inferred, which is what it is: nothing has been observed about this hop yet.
func hopConfidence(c string) string {
	switch c {
	case "observed_effect", "verified":
		return c
	default:
		return "inferred"
	}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
