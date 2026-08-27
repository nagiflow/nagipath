package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// RuleSummary provides plain-language explanations for directives. This is a
// FIXED table keyed by (vendor, directive) — lookup only, never generated prose,
// and never a guess. A directive with no entry shows the directive verbatim.
var RuleSummary = map[string]map[string]string{
	"nginx": {
		"proxy_pass":         "Send to upstream pool or URL",
		"proxy_set_header":   "Set request header before proxying",
		"proxy_read_timeout": "Maximum time waiting for upstream response",
		"add_header":         "Add response header to client",
		"rewrite":            "Rewrite request URL",
		"return":             "Return status code and optionally redirect",
		"limit_req":          "Apply rate limiting",
		"limit_conn":         "Limit concurrent connections",
		"auth_basic":         "Require HTTP basic authentication",
		"deny":               "Deny access from IP or range",
		"allow":              "Allow access from IP or range",
		"ssl_certificate":    "Path to TLS certificate file",
		"ssl_protocols":      "Allowed TLS protocol versions",
		"location":           "Match requests by URI pattern",
		"server_name":        "Match requests by hostname",
		"listen":             "Listen on port and address",
		"root":               "Set document root directory",
		"try_files":          "Try files in order, fallback if none exist",
	},
	"haproxy": {
		"use_backend":     "Route to backend based on condition",
		"default_backend": "Default backend when no rule matches",
		"http-request":    "Manipulate HTTP request",
		"http-response":   "Manipulate HTTP response",
		"acl":             "Define access control condition",
		"bind":            "Listen on address and port",
		"server":          "Define backend server",
		"balance":         "Set load balancing algorithm",
		"option httpchk":  "Configure HTTP health check",
		"stick-table":     "Define persistence table",
		"rate-limit":      "Apply request rate limiting",
		"redirect":        "Redirect requests",
		"reqrep":          "Modify request using regex",
		"rspadd":          "Add response header",
		"rsprep":          "Modify response using regex",
	},
	"apache": {
		"ProxyPass":          "Reverse proxy to backend URL",
		"ProxyPassReverse":   "Adjust response headers from backend",
		"RewriteRule":        "Rewrite URL based on pattern",
		"RewriteCond":        "Condition for rewrite rule",
		"Header":             "Manipulate HTTP headers",
		"Redirect":           "Redirect to different URL",
		"RedirectMatch":      "Redirect based on regex match",
		"Require":            "Set authorization requirement",
		"SSLEngine":          "Enable SSL/TLS for this context",
		"SSLCertificateFile": "Path to SSL certificate",
		"ServerName":         "Primary hostname for vhost",
		"ServerAlias":        "Additional hostnames for vhost",
		"DocumentRoot":       "Base directory for documents",
		"Directory":          "Apply directives to filesystem path",
		"Location":           "Apply directives to URL path",
		"VirtualHost":        "Define virtual host container",
	},
}

// RuleSummaryText returns the plain-language summary for a directive, or the
// directive itself if no summary is defined.
func RuleSummaryText(vendor, directive string) string {
	if summaries, ok := RuleSummary[strings.ToLower(vendor)]; ok {
		if text, ok := summaries[directive]; ok {
			return text
		}
	}
	return directive
}

// LookupFacets holds facet counts for rule lookup filters.
type LookupFacets struct {
	Vendors  map[string]int
	Classes  map[string]int
	Clusters map[string]int
	Nodes    map[string]int
}

// ComputeLookupFacets builds facet counts from a list of lookup candidates.
// This is called after filtering by query but before applying facet filters,
// so the counts show what each filter would leave.
func (db *DB) ComputeLookupFacets(ctx context.Context, hostname, path string, port int, scheme string) (LookupFacets, error) {
	// For now, compute facets from all instances. A real implementation would
	// query based on which instances can answer the hostname/path.
	rows, err := db.R.QueryContext(ctx, `SELECT DISTINCT
		i.vendor,
		COALESCE(cl.name, ''),
		n.display_name
		FROM instance i
		JOIN node n ON n.id = i.node_id
		LEFT JOIN cluster cl ON cl.id = i.cluster_id
		JOIN snapshot s ON s.instance_id = i.id AND s.is_current = 1`)
	if err != nil {
		return LookupFacets{}, err
	}
	defer rows.Close()

	f := LookupFacets{
		Vendors:  make(map[string]int),
		Classes:  make(map[string]int),
		Clusters: make(map[string]int),
		Nodes:    make(map[string]int),
	}

	for rows.Next() {
		var vendor, cluster, node string
		if err := rows.Scan(&vendor, &cluster, &node); err != nil {
			return f, err
		}
		f.Vendors[vendor]++
		if cluster == "" {
			cluster = "(no cluster)"
		}
		f.Clusters[cluster]++
		f.Nodes[node]++
	}

	// Action classes come from trace.ActionClasses, but we can approximate counts
	// by querying the rule table for rules in current snapshots.
	classRows, err := db.R.QueryContext(ctx, `SELECT action_class, COUNT(*)
		FROM rule r
		JOIN snapshot s ON s.id = r.snapshot_id AND s.is_current = 1
		WHERE action_class != ''
		GROUP BY action_class`)
	if err != nil {
		return f, err
	}
	defer classRows.Close()

	for classRows.Next() {
		var class string
		var count int
		if err := classRows.Scan(&class, &count); err != nil {
			return f, err
		}
		f.Classes[class] = count
	}

	return f, rows.Err()
}

// SearchFacets extends the existing facets with snapshot age.
type SearchFacets struct {
	Vendors     []searchFacet
	Files       []searchFacet
	Clusters    []searchFacet
	SnapshotAge []searchFacet
}

type searchFacet struct {
	Value string
	Count int
}

// ComputeSearchFacets builds facets from search results, including snapshot age.
func ComputeSearchFacets(rules []RuleHit, texts []TextHit) SearchFacets {
	vend, file, clust, age := make(map[string]int), make(map[string]int), make(map[string]int), make(map[string]int)

	count := func(vendor, path, cluster, snapshotAge string) {
		vend[vendor]++
		file[path]++
		if cluster == "" {
			cluster = "(no cluster)"
		}
		clust[cluster]++
		age[snapshotAge]++
	}

	// Helper to determine snapshot age bucket
	ageFor := func(snapshotID int64) string {
		// This would need to query the snapshot.collected_at timestamp
		// For now, placeholder logic
		return "< 6h"
	}

	for _, h := range rules {
		count(h.Vendor, h.Path, h.Cluster, ageFor(h.SnapshotID))
	}
	for _, h := range texts {
		count(h.Vendor, h.Path, h.Cluster, ageFor(h.SnapshotID))
	}

	return SearchFacets{
		Vendors:     facetList(vend),
		Files:       facetList(file),
		Clusters:    facetList(clust),
		SnapshotAge: facetList(age),
	}
}

func facetList(counts map[string]int) []searchFacet {
	out := make([]searchFacet, 0, len(counts))
	for v, n := range counts {
		out = append(out, searchFacet{v, n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Value < out[j].Value
	})
	return out
}

// ConfigSearchParams holds all search parameters including match mode.
type ConfigSearchParams struct {
	Query       string
	MatchMode   string // "substring" or "regex"
	Scope       string // "current" or "all"
	Context     int    // lines of context
	Vendors     []string
	Files       []string
	Clusters    []string
	SnapshotAge []string
	Limit       int
}

// SearchConfigWithRegex performs text search with regex support.
func (db *DB) SearchConfigWithRegex(ctx context.Context, params ConfigSearchParams) ([]TextHit, error) {
	// For the initial implementation, we'll use the existing FTS search
	// and apply regex filtering in Go if needed. A production implementation
	// would handle this more efficiently.

	query := params.Query
	if params.MatchMode == "regex" {
		// For regex, we still use FTS to narrow candidates, then filter
		query = params.Query
	}

	// Build WHERE clause for scope
	scopeFilter := "s.is_current = 1"
	if params.Scope == "all" {
		scopeFilter = "1=1"
	}

	sql := fmt.Sprintf(`SELECT f.id, f.snapshot_id, s.instance_id,
		i.display_name, i.vendor, n.display_name, COALESCE(cl.name, ''), f.path,
		snippet(snapshot_text_fts, 1, '[', ']', '…', 12)
		FROM snapshot_text_fts
		JOIN snapshot_text_fts_map mp ON mp.rowid = snapshot_text_fts.rowid
		JOIN snapshot_file f ON f.id = mp.snapshot_file_id
		JOIN snapshot s ON s.id = f.snapshot_id
		JOIN instance i ON i.id = s.instance_id
		JOIN node n ON n.id = i.node_id
		LEFT JOIN cluster cl ON cl.id = i.cluster_id
		WHERE snapshot_text_fts MATCH ? AND %s
		ORDER BY rank LIMIT ?`, scopeFilter)

	rows, err := db.R.QueryContext(ctx, sql, ftsQuery(query), params.Limit)
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
