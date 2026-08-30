package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

// SiteListRow is one row in the Sites list, with aggregated fleet-wide data.
type SiteListRow struct {
	Name            string
	AliasCount      int
	Aliases         []string
	Nodes           int
	ListenerSummary string // "0.0.0.0:443 ssl http2"
	CertSubject     string
	Routes          int
	Variants        int
	State           string // worst state: "CERT 6d", "OK", etc
	StateReason     string
}

// SiteVariant is one distinct configuration of a hostname across the fleet.
type SiteVariant struct {
	Key       string // A, B, C
	RawText   string // the hash key
	Nodes     int
	NodeNames []string
	Routes    []SiteVariantRoute
}

// SiteVariantRoute is one route within a variant for the detail panel comparison.
type SiteVariantRoute struct {
	Pattern  string
	Action   string
	Target   string
	Upstream string
}

// SiteListWithVariants returns the sites list with alias counts and variant data for
// the selected site's detail panel.
func (db *DB) SiteListWithVariants(ctx context.Context, selectedSite string) ([]SiteListRow, []SiteVariant, error) {
	cutoff := time.Now().UTC().Add(30 * 24 * time.Hour).Format("2006-01-02T15:04:05Z")

	// First, get the list
	rows, err := db.R.QueryContext(ctx, `
		WITH site_aliases AS (
		  SELECT s.primary_name, sn.name AS alias_name
		  FROM site s
		  JOIN snapshot snap ON snap.id = s.snapshot_id AND snap.is_current = 1
		  JOIN site_name sn ON sn.site_id = s.id
		  WHERE s.primary_name != '' AND sn.name != s.primary_name AND sn.match_kind != 'catch_all'
		)
		SELECT s.primary_name,
		       COUNT(DISTINCT sa.alias_name),
		       COALESCE(group_concat(DISTINCT sa.alias_name), ''),
		       COUNT(DISTINCT i.node_id),
		       -- Correlated on primary_name only. This used to reach through a
		       -- "JOIN site s2" self-join and match s2.id against MIN(id): in a GROUP BY
		       -- over several nodes, whichever s2 row the group happened to evaluate
		       -- against was usually not that one, so the subquery returned NULL and
		       -- the whole list 500ed the moment two hosts served the same name.
		       COALESCE((SELECT l.address || ':' || l.port ||
		          CASE WHEN l.tls = 1 THEN ' ssl' ELSE '' END ||
		          CASE WHEN l.protocol != '' THEN ' ' || l.protocol ELSE '' END
		        FROM site sl
		        JOIN snapshot sp ON sp.id = sl.snapshot_id AND sp.is_current = 1
		        JOIN json_each(sl.listener_ids) je
		        JOIN listener l ON l.id = CAST(je.value AS INTEGER)
		        WHERE sl.primary_name = s.primary_name
		        ORDER BY l.port LIMIT 1), '') AS listener_summary,
		       COALESCE(MIN(c.subject_cn), ''),
		       COUNT(DISTINCT r.id),
		       COUNT(DISTINCT s.raw_text),
		       CASE
		         WHEN MIN(c.not_after) <= ? THEN 'CERT ' ||
		           CAST(CAST((julianday(MIN(c.not_after)) - julianday('now')) AS INTEGER) AS TEXT) || 'd'
		         ELSE 'OK'
		       END AS state,
		       '' AS state_reason
		FROM site s
		JOIN snapshot snap ON snap.id = s.snapshot_id AND snap.is_current = 1
		JOIN instance i ON i.id = s.instance_id AND i.retired_at IS NULL
		LEFT JOIN site_aliases sa ON sa.primary_name = s.primary_name
		LEFT JOIN route r ON r.site_id = s.id
		LEFT JOIN certificate_binding cb ON cb.site_id = s.id AND cb.snapshot_id = snap.id
		LEFT JOIN certificate c ON c.id = cb.certificate_id
		WHERE s.primary_name != ''
		GROUP BY s.primary_name
		ORDER BY s.primary_name`, cutoff)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var list []SiteListRow
	for rows.Next() {
		var s SiteListRow
		var aliasStr string
		if err := rows.Scan(&s.Name, &s.AliasCount, &aliasStr, &s.Nodes, &s.ListenerSummary,
			&s.CertSubject, &s.Routes, &s.Variants, &s.State, &s.StateReason); err != nil {
			return nil, nil, err
		}
		if aliasStr != "" {
			s.Aliases = strings.Split(aliasStr, ",")
		}
		list = append(list, s)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	// If a site is selected, get its variants
	var variants []SiteVariant
	if selectedSite != "" {
		variants, err = db.siteVariants(ctx, selectedSite)
		if err != nil {
			return nil, nil, err
		}
	}

	return list, variants, nil
}

// siteVariants gets the distinct configurations (variants) of a site across the fleet.
func (db *DB) siteVariants(ctx context.Context, siteName string) ([]SiteVariant, error) {
	rows, err := db.R.QueryContext(ctx, `
		SELECT s.raw_text, n.display_name,
		       COALESCE(r.pattern, ''), COALESCE(r.match_type, ''), COALESCE(r.target_raw, ''), COALESCE(u.name, '')
		FROM site s
		JOIN snapshot snap ON snap.id = s.snapshot_id AND snap.is_current = 1
		JOIN instance i ON i.id = s.instance_id
		JOIN node n ON n.id = i.node_id
		LEFT JOIN route r ON r.site_id = s.id
		LEFT JOIN upstream u ON u.id = r.upstream_id
		WHERE s.primary_name = ?
		ORDER BY s.raw_text, r.precedence_rank, r.specificity DESC, r.ordinal`, siteName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Group by raw_text
	variantMap := make(map[string]*SiteVariant)
	var order []string
	for rows.Next() {
		var rawText, nodeName, pattern, matchType, target, upstream string
		if err := rows.Scan(&rawText, &nodeName, &pattern, &matchType, &target, &upstream); err != nil {
			return nil, err
		}
		if _, ok := variantMap[rawText]; !ok {
			order = append(order, rawText)
			variantMap[rawText] = &SiteVariant{RawText: rawText}
		}
		v := variantMap[rawText]
		if !contains(v.NodeNames, nodeName) {
			v.NodeNames = append(v.NodeNames, nodeName)
			v.Nodes++
		}
		if pattern != "" {
			v.Routes = append(v.Routes, SiteVariantRoute{
				Pattern:  pattern,
				Action:   matchType,
				Target:   target,
				Upstream: upstream,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Assign letters A, B, C
	var out []SiteVariant
	for i, key := range order {
		v := variantMap[key]
		v.Key = string(rune('A' + i))
		out = append(out, *v)
	}
	return out, nil
}

// SiteOverview is the data for the Overview tab on the site detail page.
type SiteOverview struct {
	Name            string
	Aliases         []string
	Variants        int
	VariantKey      string // selected variant A/B/C
	Nodes           int
	Clusters        []ClusterCount
	ListenerSummary string
	ListenerFlags   string
	CertSubject     string
	CertIssuer      string
	CertExpiry      string
	CertExpiryDays  int
	CertBindings    int
	CertUncovered   []string
	Upstreams       []UpstreamSummary
	Routes          []SiteDetailRoute
	Stats           SiteDetailStats
}

// ClusterCount is one cluster and its node count for a site.
type ClusterCount struct {
	Name  string
	Nodes int
}

// UpstreamSummary is one upstream pool reached by a site.
type UpstreamSummary struct {
	Name    string
	Members int
	Variant string // which variant uses this
}

// SiteDetailRoute is one route on a site, for the Overview tab table.
type SiteDetailRoute struct {
	Ordinal    int
	Pattern    string
	MatchType  string
	Action     string
	Target     string
	AlsoDoes   string
	Variant    string
	FileID     int64
	SnapshotID int64
	ByteStart  int
}

// SiteDetailStats are the five tiles on the Overview tab.
type SiteDetailStats struct {
	Nodes       int
	Routes      int
	Variants    int
	Upstreams   int
	CertDays    int
	CertSubject string
}

// SiteOverviewData loads the Overview tab data for a site.
func (db *DB) SiteOverviewData(ctx context.Context, name, variantKey string) (SiteOverview, error) {
	var d SiteOverview
	d.Name = name
	d.VariantKey = variantKey

	// Get aliases
	aliasRows, err := db.R.QueryContext(ctx, `
		SELECT DISTINCT sn.name FROM site_name sn
		JOIN site s ON s.id = sn.site_id
		JOIN snapshot snap ON snap.id = s.snapshot_id AND snap.is_current = 1
		WHERE sn.name != ? AND s.primary_name = ? AND sn.match_kind != 'catch_all'
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
	aliasRows.Close()

	// Get variant mapping
	variants, err := db.siteVariants(ctx, name)
	if err != nil {
		return d, err
	}
	d.Variants = len(variants)

	// If no variant selected, default to first
	if variantKey == "" && len(variants) > 0 {
		variantKey = variants[0].Key
		d.VariantKey = variantKey
	}

	var selectedRawText string
	for _, v := range variants {
		if v.Key == variantKey {
			selectedRawText = v.RawText
			break
		}
	}

	// Get stats
	err = db.R.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT i.node_id),
		       COUNT(DISTINCT r.id),
		       COUNT(DISTINCT s.raw_text),
		       COUNT(DISTINCT u.id),
		       COALESCE(MIN(CAST((julianday(c.not_after) - julianday('now')) AS INTEGER)), 999),
		       COALESCE(MIN(c.subject_cn), '')
		FROM site s
		JOIN snapshot snap ON snap.id = s.snapshot_id AND snap.is_current = 1
		JOIN instance i ON i.id = s.instance_id
		LEFT JOIN route r ON r.site_id = s.id
		LEFT JOIN upstream u ON u.id = r.upstream_id
		LEFT JOIN certificate_binding cb ON cb.site_id = s.id AND cb.snapshot_id = snap.id
		LEFT JOIN certificate c ON c.id = cb.certificate_id
		WHERE s.primary_name = ?`, name).
		Scan(&d.Stats.Nodes, &d.Stats.Routes, &d.Stats.Variants, &d.Stats.Upstreams,
			&d.Stats.CertDays, &d.Stats.CertSubject)
	if err != nil {
		return d, err
	}

	// Get cluster counts
	clusterRows, err := db.R.QueryContext(ctx, `
		SELECT COALESCE(cl.name, 'no cluster'), COUNT(DISTINCT n.id)
		FROM site s
		JOIN snapshot snap ON snap.id = s.snapshot_id AND snap.is_current = 1
		JOIN instance i ON i.id = s.instance_id
		JOIN node n ON n.id = i.node_id
		LEFT JOIN cluster cl ON cl.id = i.cluster_id
		WHERE s.primary_name = ?
		GROUP BY cl.name
		ORDER BY COUNT(DISTINCT n.id) DESC`, name)
	if err != nil {
		return d, err
	}
	for clusterRows.Next() {
		var cc ClusterCount
		if err := clusterRows.Scan(&cc.Name, &cc.Nodes); err != nil {
			clusterRows.Close()
			return d, err
		}
		d.Clusters = append(d.Clusters, cc)
		d.Nodes += cc.Nodes
	}
	clusterRows.Close()

	// Get listener summary (from first instance)
	err = db.R.QueryRowContext(ctx, `
		SELECT l.address || ':' || l.port AS summary,
		       CASE WHEN l.tls = 1 THEN 'ssl' ELSE '' END ||
		       CASE WHEN l.protocol != '' THEN ' ' || l.protocol ELSE '' END AS flags
		FROM site s
		JOIN snapshot snap ON snap.id = s.snapshot_id AND snap.is_current = 1
		JOIN json_each(s.listener_ids) je ON true
		JOIN listener l ON l.id = CAST(je.value AS INTEGER)
		WHERE s.primary_name = ?
		LIMIT 1`, name).Scan(&d.ListenerSummary, &d.ListenerFlags)
	if err != nil && err != sql.ErrNoRows {
		return d, err
	}

	// Get certificate info
	err = db.R.QueryRowContext(ctx, `
		SELECT c.subject_cn, c.issuer_dn, c.not_after,
		       COUNT(DISTINCT cb.site_id)
		FROM certificate c
		JOIN certificate_binding cb ON cb.certificate_id = c.id
		JOIN snapshot snap ON snap.id = cb.snapshot_id AND snap.is_current = 1
		JOIN site s ON s.id = cb.site_id
		WHERE s.primary_name = ?
		GROUP BY c.id
		ORDER BY c.not_after ASC
		LIMIT 1`, name).Scan(&d.CertSubject, &d.CertIssuer, &d.CertExpiry, &d.CertBindings)
	if err != nil && err != sql.ErrNoRows {
		return d, err
	}
	if d.CertExpiry != "" {
		t, _ := time.Parse(time.RFC3339, d.CertExpiry)
		d.CertExpiryDays = int(time.Until(t).Hours() / 24)
	}

	// Get upstreams
	upstreamRows, err := db.R.QueryContext(ctx, `
		SELECT DISTINCT u.name, COUNT(DISTINCT um.id), s.raw_text
		FROM site s
		JOIN snapshot snap ON snap.id = s.snapshot_id AND snap.is_current = 1
		JOIN route r ON r.site_id = s.id
		JOIN upstream u ON u.id = r.upstream_id
		LEFT JOIN upstream_member um ON um.upstream_id = u.id
		WHERE s.primary_name = ?
		GROUP BY u.name, s.raw_text`, name)
	if err != nil {
		return d, err
	}
	for upstreamRows.Next() {
		var us UpstreamSummary
		var rawText string
		if err := upstreamRows.Scan(&us.Name, &us.Members, &rawText); err != nil {
			upstreamRows.Close()
			return d, err
		}
		// Map raw_text to variant key
		for _, v := range variants {
			if v.RawText == rawText {
				us.Variant = v.Key
				break
			}
		}
		d.Upstreams = append(d.Upstreams, us)
	}
	upstreamRows.Close()

	// Get routes for selected variant. Scoped to one representative site row
	// (the lowest id matching this hostname+raw_text+current snapshot), not
	// every site row that shares the variant: two nodes running byte-identical
	// config each parse their own site+route rows, and joining across all of
	// them would return the same context path once per node instead of once.
	if selectedRawText != "" {
		routeRows, err := db.R.QueryContext(ctx, `
			-- Older databases could contain a NULL route pattern for a catch-all
			-- site (underscore). Keep the detail page readable while migrations converge
			-- those historical rows on the current NOT NULL schema.
			SELECT r.ordinal, COALESCE(r.pattern, ''), COALESCE(r.match_type, ''), COALESCE(r.target_raw, ''),
			       COALESCE(u.name, ''), r.prov_file_id, r.snapshot_id, r.prov_byte_start
			FROM route r
			LEFT JOIN upstream u ON u.id = r.upstream_id
			WHERE r.site_id = (
				SELECT s.id FROM site s
				JOIN snapshot snap ON snap.id = s.snapshot_id AND snap.is_current = 1
				WHERE s.primary_name = ? AND s.raw_text = ?
				ORDER BY s.id LIMIT 1
			)
			ORDER BY r.precedence_rank, r.specificity DESC, r.ordinal`, name, selectedRawText)
		if err != nil {
			return d, err
		}
		for routeRows.Next() {
			var rt SiteDetailRoute
			if err := routeRows.Scan(&rt.Ordinal, &rt.Pattern, &rt.MatchType, &rt.Target,
				&rt.Action, &rt.FileID, &rt.SnapshotID, &rt.ByteStart); err != nil {
				routeRows.Close()
				return d, err
			}
			rt.Variant = variantKey
			d.Routes = append(d.Routes, rt)
		}
		routeRows.Close()
	}

	return d, nil
}

// SiteNodeRow is one node serving a site, for the Nodes tab.
type SiteNodeRow struct {
	NodeID      int64
	NodeName    string
	Cluster     string
	Variant     string
	Listener    string
	Certificate string
	LastColl    string
	State       string
	StateReason string
}

// SiteNodesData loads the Nodes tab data.
func (db *DB) SiteNodesData(ctx context.Context, name string) ([]SiteNodeRow, error) {
	variants, err := db.siteVariants(ctx, name)
	if err != nil {
		return nil, err
	}
	variantMap := make(map[string]string)
	for _, v := range variants {
		variantMap[v.RawText] = v.Key
	}

	rows, err := db.R.QueryContext(ctx, `
		SELECT n.id, n.display_name, COALESCE(cl.name, ''), s.raw_text,
		       (SELECT l.address || ':' || l.port ||
		          CASE WHEN l.tls = 1 THEN ' ssl' ELSE '' END ||
		          CASE WHEN l.protocol != '' THEN ' ' || l.protocol ELSE '' END
		        FROM listener l
		        JOIN json_each(s.listener_ids) je ON l.id = CAST(je.value AS INTEGER)
		        LIMIT 1),
		       COALESCE(c.subject_cn, ''),
		       snap.captured_at,
		       'OK', ''
		FROM site s
		JOIN snapshot snap ON snap.id = s.snapshot_id AND snap.is_current = 1
		JOIN instance i ON i.id = s.instance_id
		JOIN node n ON n.id = i.node_id
		LEFT JOIN cluster cl ON cl.id = i.cluster_id
		LEFT JOIN certificate_binding cb ON cb.site_id = s.id AND cb.snapshot_id = snap.id
		LEFT JOIN certificate c ON c.id = cb.certificate_id
		WHERE s.primary_name = ?
		GROUP BY n.id, n.display_name, cl.name, s.raw_text, snap.captured_at
		ORDER BY n.display_name`, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SiteNodeRow
	for rows.Next() {
		var r SiteNodeRow
		var rawText string
		if err := rows.Scan(&r.NodeID, &r.NodeName, &r.Cluster, &rawText, &r.Listener,
			&r.Certificate, &r.LastColl, &r.State, &r.StateReason); err != nil {
			return nil, err
		}
		r.Variant = variantMap[rawText]
		out = append(out, r)
	}
	return out, rows.Err()
}

// SiteUpstreamMember is one member of an upstream pool, for the Upstreams tab.
type SiteUpstreamMember struct {
	Upstream string
	Host     string
	Port     int
	Scheme   string
	Weight   int
	Flags    string
	NodeName string // resolved node
}

// SiteUpstreamsData loads the Upstreams tab data.
func (db *DB) SiteUpstreamsData(ctx context.Context, name string) ([]SiteUpstreamMember, error) {
	rows, err := db.R.QueryContext(ctx, `
		-- port and weight are nullable and normally null: unset means "the scheme
		-- default" and "the balancer default". Zero is how the template says so.
		SELECT DISTINCT u.name, um.host, COALESCE(um.port, 0), um.scheme, COALESCE(um.weight, 0), um.flags,
		       COALESCE(resolved.display_name, '')
		FROM site s
		JOIN snapshot snap ON snap.id = s.snapshot_id AND snap.is_current = 1
		JOIN route r ON r.site_id = s.id
		JOIN upstream u ON u.id = r.upstream_id
		JOIN upstream_member um ON um.upstream_id = u.id
		LEFT JOIN node resolved ON resolved.address = um.host
		WHERE s.primary_name = ?
		ORDER BY u.name, um.ordinal`, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SiteUpstreamMember
	for rows.Next() {
		var m SiteUpstreamMember
		if err := rows.Scan(&m.Upstream, &m.Host, &m.Port, &m.Scheme, &m.Weight, &m.Flags, &m.NodeName); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SiteCertBinding is one certificate bound to a site, for the Certificates tab.
type SiteCertBinding struct {
	Subject    string
	SANs       []string
	Issuer     string
	NotAfter   string
	ExpiryDays int
	Bindings   int
	Uncovered  []string
}

// SiteCertificatesData loads the Certificates tab data.
func (db *DB) SiteCertificatesData(ctx context.Context, name string) ([]SiteCertBinding, error) {
	rows, err := db.R.QueryContext(ctx, `
		SELECT DISTINCT c.subject_cn, c.sans, c.issuer_dn, c.not_after,
		       COUNT(DISTINCT cb.id)
		FROM certificate c
		JOIN certificate_binding cb ON cb.certificate_id = c.id
		JOIN snapshot snap ON snap.id = cb.snapshot_id AND snap.is_current = 1
		JOIN site s ON s.id = cb.site_id
		WHERE s.primary_name = ?
		GROUP BY c.id
		ORDER BY c.not_after ASC`, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SiteCertBinding
	for rows.Next() {
		var b SiteCertBinding
		var sansJSON string
		if err := rows.Scan(&b.Subject, &sansJSON, &b.Issuer, &b.NotAfter, &b.Bindings); err != nil {
			return nil, err
		}
		if sansJSON != "" {
			json.Unmarshal([]byte(sansJSON), &b.SANs)
		}
		if b.NotAfter != "" {
			t, _ := time.Parse(time.RFC3339, b.NotAfter)
			b.ExpiryDays = int(time.Until(t).Hours() / 24)
		}
		// Check coverage
		names := append([]string{b.Subject}, b.SANs...)
		if !coveredBySANs(name, names) {
			b.Uncovered = append(b.Uncovered, name)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

func coveredBySANs(hostname string, names []string) bool {
	hostname = strings.ToLower(strings.TrimSuffix(hostname, "."))
	for _, n := range names {
		n = strings.ToLower(strings.TrimSuffix(n, "."))
		if n == hostname {
			return true
		}
		// Wildcard check
		if strings.HasPrefix(n, "*.") {
			suffix := strings.TrimPrefix(n, "*.")
			if strings.HasSuffix(hostname, "."+suffix) {
				parts := strings.Split(hostname, ".")
				if len(parts) == strings.Count(suffix, ".")+2 {
					return true
				}
			}
		}
	}
	return false
}
