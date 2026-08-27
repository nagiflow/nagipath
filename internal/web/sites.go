package web

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/nagiflow/nagipath/internal/store"
)

type sitesData struct {
	Rows     []store.SiteListRow
	Query    string
	Total    int
	Stats    store.SiteStats
	Sel      string
	Variants []store.SiteVariant
}

func (s *Server) sites(w http.ResponseWriter, r *http.Request) {
	selected := strings.TrimSpace(r.URL.Query().Get("site"))
	rows, variants, err := s.DB.SiteListWithVariants(r.Context(), selected)
	if err != nil {
		s.serverError(w, r, err)
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

	stats, err := s.DB.SitesStats(r.Context())
	if err != nil {
		s.serverError(w, r, err)
		return
	}

	d := sitesData{Rows: rows, Query: q, Total: len(rows), Stats: stats, Sel: selected, Variants: variants}

	if r.URL.Query().Get("export") == "csv" {
		out := [][]string{{"hostname", "aliases", "nodes", "listener", "certificate", "routes", "variants", "state"}}
		for _, row := range rows {
			out = append(out, []string{
				row.Name,
				strconv.Itoa(row.AliasCount),
				strconv.Itoa(row.Nodes),
				row.ListenerSummary,
				row.CertSubject,
				strconv.Itoa(row.Routes),
				strconv.Itoa(row.Variants),
				row.State,
			})
		}
		s.writeCSV(w, r, "sites", out)
		return
	}
	s.render(w, r, "sites.html", "Sites", d)
}

type siteDetailData struct {
	Name        string
	Tab         string
	Variant     string
	Overview    *store.SiteOverview
	Nodes       []store.SiteNodeRow
	Upstreams   []store.SiteUpstreamMember
	Certs       []store.SiteCertBinding
	Tabs        []navItem
	VariantOpts []variantOpt
}

type variantOpt struct {
	Key   string
	Label string
	On    bool
}

func (s *Server) site(w http.ResponseWriter, r *http.Request) {
	name, err := url.PathUnescape(r.PathValue("name"))
	if err != nil || strings.TrimSpace(name) == "" {
		s.notFound(w, r)
		return
	}

	tab := r.URL.Query().Get("tab")
	if tab == "" {
		tab = "overview"
	}
	variant := r.URL.Query().Get("variant")

	var d siteDetailData
	d.Name = name
	d.Tab = tab
	d.Variant = variant

	// A query error is a query error. Folding it into notFound hid a wrong column
	// name behind a plausible 404 for every site in the fleet — the page looked
	// like a typo in the URL rather than a bug in the SQL.
	overview, err := s.DB.SiteOverviewData(r.Context(), name, variant)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	// No node in any current snapshot serves this hostname, so there is nothing to
	// show and nothing to say about it.
	if overview.Stats.Nodes == 0 {
		s.notFound(w, r)
		return
	}

	// Build tabs
	d.Tabs = []navItem{
		{Label: "Overview", Href: "/sites/" + url.PathEscape(name), On: tab == "overview"},
		{Label: "Nodes", Href: "/sites/" + url.PathEscape(name) + "?tab=nodes", Count: overview.Nodes, On: tab == "nodes"},
		{Label: "Upstreams", Href: "/sites/" + url.PathEscape(name) + "?tab=upstreams", Count: overview.Stats.Upstreams, On: tab == "upstreams"},
		{Label: "Certificates", Href: "/sites/" + url.PathEscape(name) + "?tab=certificates", On: tab == "certificates"},
	}

	// Build variant selector
	for i := 0; i < overview.Variants; i++ {
		key := string(rune('A' + i))
		d.VariantOpts = append(d.VariantOpts, variantOpt{
			Key:   key,
			Label: "Variant " + key,
			On:    key == overview.VariantKey,
		})
	}

	// Load tab-specific data
	switch tab {
	case "nodes":
		nodes, err := s.DB.SiteNodesData(r.Context(), name)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		d.Nodes = nodes
	case "upstreams":
		upstreams, err := s.DB.SiteUpstreamsData(r.Context(), name)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		d.Upstreams = upstreams
	case "certificates":
		certs, err := s.DB.SiteCertificatesData(r.Context(), name)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		d.Certs = certs
	default:
		d.Overview = &overview
		// The overview carries the "Nodes serving this site" panel as well as the
		// Nodes tab: the wireframe puts both on screen, and the panel is the only
		// place the page names which node is on which variant.
		nodes, err := s.DB.SiteNodesData(r.Context(), name)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		d.Nodes = nodes
	}

	s.render(w, r, "site.html", name, d)
}
