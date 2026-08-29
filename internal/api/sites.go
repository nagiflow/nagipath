package api

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/nagiflow/nagipath/internal/store"
)

// Sites' response fields reuse store types' exported (PascalCase) field
// names as-is rather than hand-writing snake_case DTOs — unlike dashboard.go
// and clusters.go, there's no computed/aggregated logic here worth a purpose
// -built struct, just a pass-through of what the store already returns.

type sitesListResponse struct {
	Rows     []store.SiteListRow `json:"Rows"`
	Query    string              `json:"Query"`
	Total    int                 `json:"Total"`
	Stats    store.SiteStats     `json:"Stats"`
	Sel      string              `json:"Sel"`
	Variants []store.SiteVariant `json:"Variants"`
}

// getSites ports internal/web/sites.go's sites(): same ?q= substring filter
// over name/listener, same ?site= variant-comparison selection, same
// ?export=csv download.
func (s *Server) getSites(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	selected := strings.TrimSpace(r.URL.Query().Get("site"))
	rows, variants, err := s.DB.SiteListWithVariants(ctx, selected)
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
	if rows == nil {
		rows = []store.SiteListRow{}
	}
	if variants == nil {
		variants = []store.SiteVariant{}
	}

	if r.URL.Query().Get("export") == "csv" {
		out := [][]string{{"hostname", "aliases", "nodes", "listener", "certificate", "routes", "variants", "state"}}
		for _, row := range rows {
			out = append(out, []string{
				row.Name, strconv.Itoa(row.AliasCount), strconv.Itoa(row.Nodes),
				row.ListenerSummary, row.CertSubject, strconv.Itoa(row.Routes),
				strconv.Itoa(row.Variants), row.State,
			})
		}
		s.writeCSV(w, r, "sites", out)
		return
	}

	stats, err := s.DB.SitesStats(ctx)
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, sitesListResponse{
		Rows: rows, Query: q, Total: len(rows), Stats: stats, Sel: selected, Variants: variants,
	})
}

type siteTabItem struct {
	Label string `json:"Label"`
	Href  string `json:"Href"`
	Count int    `json:"Count"`
	On    bool   `json:"On"`
}

type siteVariantOpt struct {
	Key   string `json:"Key"`
	Label string `json:"Label"`
	On    bool   `json:"On"`
}

type siteDetailResponse struct {
	Name        string                     `json:"Name"`
	Tab         string                     `json:"Tab"`
	Variant     string                     `json:"Variant"`
	Overview    *store.SiteOverview        `json:"Overview,omitempty"`
	Nodes       []store.SiteNodeRow        `json:"Nodes,omitempty"`
	Upstreams   []store.SiteUpstreamMember `json:"Upstreams,omitempty"`
	Certs       []store.SiteCertBinding    `json:"Certs,omitempty"`
	Tabs        []siteTabItem              `json:"Tabs"`
	VariantOpts []siteVariantOpt           `json:"VariantOpts"`
}

// getSite ports internal/web/sites.go's site(): same tab switch
// (overview/nodes/upstreams/certificates), same variant selector.
func (s *Server) getSite(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	name, err := url.PathUnescape(r.PathValue("name"))
	if err != nil || strings.TrimSpace(name) == "" {
		apiError(w, http.StatusNotFound, "not_found", "No such site.")
		return
	}

	tab := r.URL.Query().Get("tab")
	if tab == "" {
		tab = "overview"
	}
	variant := r.URL.Query().Get("variant")

	overview, err := s.DB.SiteOverviewData(ctx, name, variant)
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
		return
	}
	if overview.Stats.Nodes == 0 {
		apiError(w, http.StatusNotFound, "not_found", "No node in any current snapshot serves this hostname.")
		return
	}

	resp := siteDetailResponse{Name: name, Tab: tab, Variant: variant}
	resp.Tabs = []siteTabItem{
		{Label: "Overview", Href: "/sites/" + url.PathEscape(name), On: tab == "overview"},
		{Label: "Nodes", Href: "/sites/" + url.PathEscape(name) + "?tab=nodes", Count: overview.Nodes, On: tab == "nodes"},
		{Label: "Upstreams", Href: "/sites/" + url.PathEscape(name) + "?tab=upstreams", Count: overview.Stats.Upstreams, On: tab == "upstreams"},
		{Label: "Certificates", Href: "/sites/" + url.PathEscape(name) + "?tab=certificates", On: tab == "certificates"},
	}
	for i := range overview.Variants {
		key := string(rune('A' + i))
		resp.VariantOpts = append(resp.VariantOpts, siteVariantOpt{
			Key: key, Label: "Variant " + key, On: key == overview.VariantKey,
		})
	}

	switch tab {
	case "nodes":
		nodes, err := s.DB.SiteNodesData(ctx, name)
		if err != nil {
			apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
			return
		}
		resp.Nodes = nodes
	case "upstreams":
		upstreams, err := s.DB.SiteUpstreamsData(ctx, name)
		if err != nil {
			apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
			return
		}
		resp.Upstreams = upstreams
	case "certificates":
		certs, err := s.DB.SiteCertificatesData(ctx, name)
		if err != nil {
			apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
			return
		}
		resp.Certs = certs
	default:
		resp.Overview = &overview
		nodes, err := s.DB.SiteNodesData(ctx, name)
		if err != nil {
			apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
			return
		}
		resp.Nodes = nodes
	}

	writeJSON(w, http.StatusOK, resp)
}
