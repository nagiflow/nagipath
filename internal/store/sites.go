package store

import (
	"context"
	"time"
)

type SiteStats struct {
	Hostnames        int
	Nodes            int
	VariantHosts     int
	TLSTerminated    int
	Plaintext        int
	ExpiringCerts    int
	ExpiringBindings int
}

// SitesStats supplies the five fleet-wide figures in the Sites header. They are
// calculated over the same current-snapshot relation as Sites, so the tiles and
// rows cannot disagree about freshness.
func (db *DB) SitesStats(ctx context.Context) (SiteStats, error) {
	var s SiteStats
	cutoff := time.Now().UTC().Add(30 * 24 * time.Hour).Format("2006-01-02T15:04:05Z")
	err := db.R.QueryRowContext(ctx, `
		WITH cur AS (
		  SELECT s.raw_text, s.listener_ids, s.primary_name, i.node_id
		  FROM site s
		  JOIN snapshot snap ON snap.id = s.snapshot_id AND snap.is_current = 1
		  JOIN instance i ON i.id = s.instance_id AND i.retired_at IS NULL
		  WHERE s.primary_name != ''
		), per_name AS (
		  SELECT primary_name AS name, COUNT(DISTINCT raw_text) AS variants FROM cur GROUP BY primary_name
		), tls_names AS (
		  -- Grouped once, rather than a DISTINCT over the whole site x listener
		  -- fan-out that the hostname and variant counts then re-scanned.
		  SELECT c.primary_name AS name, MAX(l.tls) AS tls
		  FROM cur c
		  JOIN json_each(c.listener_ids) je
		  JOIN listener l ON l.id = CAST(je.value AS INTEGER)
		  GROUP BY c.primary_name
		)
		SELECT
		  (SELECT COUNT(*) FROM per_name),
		  (SELECT COUNT(DISTINCT node_id) FROM cur),
		  (SELECT COUNT(*) FROM per_name WHERE variants > 1),
		  (SELECT COUNT(*) FROM tls_names WHERE tls = 1),
		  (SELECT COUNT(DISTINCT c.id) FROM certificate c
		     JOIN certificate_binding cb ON cb.certificate_id = c.id
		     JOIN snapshot snap ON snap.id = cb.snapshot_id AND snap.is_current = 1
		     WHERE c.not_after <= ?),
		  (SELECT COUNT(*) FROM certificate c
		     JOIN certificate_binding cb ON cb.certificate_id = c.id
		     JOIN snapshot snap ON snap.id = cb.snapshot_id AND snap.is_current = 1
		     WHERE c.not_after <= ?)`, cutoff, cutoff).
		Scan(&s.Hostnames, &s.Nodes, &s.VariantHosts, &s.TLSTerminated,
			&s.ExpiringCerts, &s.ExpiringBindings)
	// tls_names is a subset of names, so the plaintext hostnames are the rest.
	// Asking SQLite for them with NOT IN re-scanned the CTE per hostname.
	s.Plaintext = s.Hostnames - s.TLSTerminated
	return s, err
}
