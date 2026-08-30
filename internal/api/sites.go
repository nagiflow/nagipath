package api

import (
	"net/http"
	"strconv"
	"strings"
)

// getSites and getSite moved to siteservice.go as SiteService's ListSites
// and GetSite RPCs (docs/adr/0018, proto/nagipath/api/v1/sites.proto).
// getSitesCSV stays here: GET /sites?export=csv is a formatted download, not
// RPC-shaped data (gateway.go's gatewayOrCSV, wired in api.go).
func (s *Server) getSitesCSV(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	selected := strings.TrimSpace(r.URL.Query().Get("site"))
	rows, _, err := s.DB.SiteListWithVariants(ctx, selected)
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
		return
	}

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q != "" {
		needle := strings.ToLower(q)
		filtered := rows[:0]
		for _, row := range rows {
			if strings.Contains(strings.ToLower(row.Name), needle) ||
				strings.Contains(strings.ToLower(row.ListenerSummary), needle) {
				filtered = append(filtered, row)
			}
		}
		rows = filtered
	}

	out := [][]string{{"hostname", "aliases", "nodes", "listener", "certificate", "routes", "variants", "state"}}
	for _, row := range rows {
		out = append(out, []string{
			row.Name, strconv.Itoa(row.AliasCount), strconv.Itoa(row.Nodes),
			row.ListenerSummary, row.CertSubject, strconv.Itoa(row.Routes),
			strconv.Itoa(row.Variants), row.State,
		})
	}
	s.writeCSV(w, r, "sites", out)
}
