package store

import (
	"context"
	"time"
)

// NavCount is the set of figures the left navigation carries on every screen.
// The design shows them everywhere, so they are one struct filled by one call
// rather than five queries scattered across handlers.
//
// Every field is a count, every query is a COUNT over an indexed column, and a
// failure yields a zero rather than an error: a number missing from the nav is
// a cosmetic problem, and refusing to render Trace because a COUNT failed is
// not.
type NavCount struct {
	Nodes    int
	Sites    int
	Clusters int
	// Drift is instances whose most recent Drift run still has findings that no
	// ignore rule covers — the number of things an operator would have to look
	// at, not the number of findings.
	Drift int
	// Certificates is certificates in use that expire within CertHorizon.
	Certificates int
}

// CertHorizon is how far ahead the nav's certificate count looks. Thirty days
// is the shortest notice that still leaves room to renew through a change
// process rather than at 3am.
const CertHorizon = 30 * 24 * time.Hour

func (db *DB) NavCounts(ctx context.Context) NavCount {
	var c NavCount
	scan := func(dst *int, query string, args ...any) {
		_ = db.R.QueryRowContext(ctx, query, args...).Scan(dst)
	}
	scan(&c.Nodes, `SELECT COUNT(*) FROM node`)
	scan(&c.Sites, `SELECT COUNT(DISTINCT s.primary_name)
		FROM site s
		JOIN snapshot snap ON snap.id = s.snapshot_id AND snap.is_current = 1
		WHERE s.primary_name != ''`)
	scan(&c.Clusters, `SELECT COUNT(*) FROM cluster`)
	// ponytail: latest run per instance is MAX(id), which ignores baseline_kind —
	// an instance compared against both its previous snapshot and its golden peer
	// counts once, under whichever ran last. That matches what /drift shows by
	// default. Split by baseline_kind here if the two ever need separate counts.
	scan(&c.Drift, `
		SELECT COUNT(DISTINCT r.instance_id)
		FROM drift_run r
		JOIN (SELECT instance_id, MAX(id) AS id FROM drift_run GROUP BY instance_id) latest
		  ON latest.id = r.id
		JOIN drift_finding f ON f.drift_run_id = r.id AND f.ignored_by_rule_id IS NULL`)
	// Bound, because an expiring certificate nobody serves is not an exposure.
	scan(&c.Certificates, `
		SELECT COUNT(DISTINCT c.id)
		FROM certificate c
		JOIN certificate_binding b ON b.certificate_id = c.id
		WHERE c.not_after <= ?`,
		time.Now().UTC().Add(CertHorizon).Format("2006-01-02T15:04:05Z"))
	return c
}
