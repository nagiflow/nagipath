package store

import (
	"context"
	"sort"
	"strings"
	"time"
)

// SiteRow is the fleet-wide view of one hostname. Sites are derived from the
// current snapshots; they are not another object a collector has to maintain.
// Keeping this query here makes the Sites screen use the same current-snapshot
// boundary as the rest of the inventory screens.
type SiteRow struct {
	Name          string
	Nodes         int
	Instances     int
	Variants      int
	Routes        int
	ExpiringCerts int
	Ports         string
	LastCollected string
}

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
		WITH current_sites AS (
		  SELECT site.id, site.instance_id, site.raw_text, site.listener_ids, site.primary_name
		  FROM site JOIN snapshot snap ON snap.id = site.snapshot_id AND snap.is_current = 1
		  JOIN instance i ON i.id = site.instance_id AND i.retired_at IS NULL
		), names AS (
		  SELECT cs.primary_name AS name, cs.id AS site_id, cs.instance_id, cs.raw_text, cs.listener_ids
		  FROM current_sites cs WHERE cs.primary_name != ''
		), tls_names AS (
		  SELECT DISTINCT names.name FROM names
		  JOIN json_each(names.listener_ids) je
		  JOIN listener l ON l.id = CAST(je.value AS INTEGER) AND l.tls = 1
		), variant_names AS (
		  SELECT name FROM names GROUP BY name HAVING COUNT(DISTINCT raw_text) > 1
		)
		SELECT
		  (SELECT COUNT(DISTINCT name) FROM names),
		  (SELECT COUNT(DISTINCT i.node_id) FROM names JOIN instance i ON i.id = names.instance_id),
		  (SELECT COUNT(*) FROM variant_names),
		  (SELECT COUNT(*) FROM tls_names),
		  (SELECT COUNT(DISTINCT name) FROM names WHERE name NOT IN (SELECT name FROM tls_names)),
		  (SELECT COUNT(DISTINCT c.id) FROM certificate c
		     JOIN certificate_binding cb ON cb.certificate_id = c.id
		     JOIN snapshot snap ON snap.id = cb.snapshot_id AND snap.is_current = 1
		     WHERE c.not_after <= ?),
		  (SELECT COUNT(*) FROM certificate c
		     JOIN certificate_binding cb ON cb.certificate_id = c.id
		     JOIN snapshot snap ON snap.id = cb.snapshot_id AND snap.is_current = 1
		     WHERE c.not_after <= ?)`, cutoff, cutoff).
		Scan(&s.Hostnames, &s.Nodes, &s.VariantHosts, &s.TLSTerminated, &s.Plaintext,
			&s.ExpiringCerts, &s.ExpiringBindings)
	return s, err
}

// Sites returns distinct hostnames across current, parsed snapshots.
func (db *DB) Sites(ctx context.Context) ([]SiteRow, error) {
	cutoff := time.Now().UTC().Add(30 * 24 * time.Hour).Format("2006-01-02T15:04:05Z")
	rows, err := db.R.QueryContext(ctx, `
		SELECT s.primary_name,
		       COUNT(DISTINCT i.node_id),
		       COUNT(DISTINCT s.instance_id),
		       COUNT(DISTINCT s.raw_text),
		       COUNT(DISTINCT r.id),
		       COUNT(DISTINCT CASE WHEN c.not_after <= ? THEN c.id END),
		       COALESCE(group_concat(DISTINCT l.port), ''),
		       MAX(snap.captured_at)
		FROM site s
		JOIN snapshot snap ON snap.id = s.snapshot_id AND snap.is_current = 1
		JOIN instance i ON i.id = s.instance_id AND i.retired_at IS NULL
		JOIN node n ON n.id = i.node_id
		LEFT JOIN route r ON r.site_id = s.id
		LEFT JOIN certificate_binding cb ON cb.site_id = s.id AND cb.snapshot_id = snap.id
		LEFT JOIN certificate c ON c.id = cb.certificate_id
		LEFT JOIN json_each(s.listener_ids) je ON true
		LEFT JOIN listener l ON l.id = CAST(je.value AS INTEGER)
		WHERE s.primary_name != ''
		GROUP BY s.primary_name
		ORDER BY s.primary_name`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SiteRow
	for rows.Next() {
		var s SiteRow
		if err := rows.Scan(&s.Name, &s.Nodes, &s.Instances, &s.Variants, &s.Routes,
			&s.ExpiringCerts, &s.Ports, &s.LastCollected); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SiteBinding is one current instance serving a hostname.
type SiteBinding struct {
	SiteID       int64
	InstanceID   int64
	NodeID       int64
	NodeName     string
	InstanceName string
	Vendor       string
	Version      string
	Cluster      string
	Ports        string
	Variant      string
	CapturedAt   string
	CertSubject  string
	CertNotAfter string
}

// SiteRoute is a route projection for a hostname. Provenance remains attached
// so the detail screen can go straight to the source file and byte range.
type SiteRoute struct {
	InstanceID int64
	NodeName   string
	Pattern    string
	MatchType  string
	Target     string
	Upstream   string
	FileID     int64
	SnapshotID int64
	ByteStart  int
}

type SiteDetail struct {
	Name     string
	Aliases  []string
	Bindings []SiteBinding
	Routes   []SiteRoute
}

// SiteDetail loads one hostname and all of its current fleet-wide variants.
func (db *DB) SiteDetail(ctx context.Context, name string) (SiteDetail, error) {
	var d SiteDetail
	if err := db.R.QueryRowContext(ctx, `
		SELECT 1 FROM site_name sn
		JOIN site s ON s.id = sn.site_id
		JOIN snapshot snap ON snap.id = s.snapshot_id AND snap.is_current = 1
		WHERE sn.name = ? AND sn.match_kind != 'catch_all' LIMIT 1`, name).Scan(new(int)); err != nil {
		return d, err
	}
	d.Name = name

	aliasRows, err := db.R.QueryContext(ctx, `
		SELECT DISTINCT sn.name FROM site_name sn
		JOIN site s ON s.id = sn.site_id
		JOIN snapshot snap ON snap.id = s.snapshot_id AND snap.is_current = 1
		WHERE sn.name != ? AND sn.match_kind != 'catch_all'
		  AND EXISTS (
			SELECT 1 FROM site_name wanted
			JOIN site ws ON ws.id = wanted.site_id
			JOIN snapshot wsn ON wsn.id = ws.snapshot_id AND wsn.is_current = 1
			WHERE wanted.name = ? AND ws.instance_id = s.instance_id)
		ORDER BY sn.name`, name, name)
	if err != nil {
		return d, err
	}
	for aliasRows.Next() {
		var alias string
		if err := aliasRows.Scan(&alias); err != nil {
			aliasRows.Close()
			return d, err
		}
		d.Aliases = append(d.Aliases, alias)
	}
	if err := aliasRows.Err(); err != nil {
		aliasRows.Close()
		return d, err
	}
	aliasRows.Close()

	bindings, err := db.R.QueryContext(ctx, `
		SELECT s.id, i.id, n.id, n.display_name, i.display_name, i.vendor,
		       i.version, COALESCE(cl.name, ''), COALESCE(group_concat(DISTINCT l.port), ''),
		       s.natural_key, snap.captured_at,
		       COALESCE(group_concat(DISTINCT c.subject_cn), ''),
		       COALESCE(MIN(c.not_after), '')
		FROM site_name sn
		JOIN site s ON s.id = sn.site_id
		JOIN snapshot snap ON snap.id = s.snapshot_id AND snap.is_current = 1
		JOIN instance i ON i.id = s.instance_id
		JOIN node n ON n.id = i.node_id
		LEFT JOIN cluster cl ON cl.id = i.cluster_id
		LEFT JOIN json_each(s.listener_ids) je ON true
		LEFT JOIN listener l ON l.id = CAST(je.value AS INTEGER)
		LEFT JOIN certificate_binding cb ON cb.site_id = s.id AND cb.snapshot_id = snap.id
		LEFT JOIN certificate c ON c.id = cb.certificate_id
		WHERE sn.name = ?
		GROUP BY s.id, i.id, n.id, n.display_name, i.display_name, i.vendor,
		         i.version, cl.name, s.natural_key, snap.captured_at
		ORDER BY n.display_name, i.display_name`, name)
	if err != nil {
		return d, err
	}
	for bindings.Next() {
		var b SiteBinding
		if err := bindings.Scan(&b.SiteID, &b.InstanceID, &b.NodeID, &b.NodeName,
			&b.InstanceName, &b.Vendor, &b.Version, &b.Cluster, &b.Ports, &b.Variant,
			&b.CapturedAt, &b.CertSubject, &b.CertNotAfter); err != nil {
			bindings.Close()
			return d, err
		}
		d.Bindings = append(d.Bindings, b)
	}
	if err := bindings.Err(); err != nil {
		bindings.Close()
		return d, err
	}
	bindings.Close()

	routes, err := db.R.QueryContext(ctx, `
		SELECT r.instance_id, n.display_name, r.pattern, r.match_type, r.target_raw,
		       COALESCE(u.name, ''), r.prov_file_id, r.snapshot_id, r.prov_byte_start
		FROM site_name sn
		JOIN site s ON s.id = sn.site_id
		JOIN snapshot snap ON snap.id = s.snapshot_id AND snap.is_current = 1
		JOIN route r ON r.site_id = s.id
		JOIN instance i ON i.id = r.instance_id
		JOIN node n ON n.id = i.node_id
		LEFT JOIN upstream u ON u.id = r.upstream_id
		WHERE sn.name = ?
		ORDER BY r.precedence_rank, r.specificity DESC, n.display_name, r.ordinal`, name)
	if err != nil {
		return d, err
	}
	for routes.Next() {
		var r SiteRoute
		if err := routes.Scan(&r.InstanceID, &r.NodeName, &r.Pattern, &r.MatchType,
			&r.Target, &r.Upstream, &r.FileID, &r.SnapshotID, &r.ByteStart); err != nil {
			routes.Close()
			return d, err
		}
		d.Routes = append(d.Routes, r)
	}
	if err := routes.Err(); err != nil {
		routes.Close()
		return d, err
	}
	routes.Close()

	// group_concat order is intentionally not relied on for display. Keep the
	// detail stable even when SQLite changes its query plan.
	sort.Slice(d.Aliases, func(i, j int) bool { return strings.ToLower(d.Aliases[i]) < strings.ToLower(d.Aliases[j]) })
	return d, nil
}
