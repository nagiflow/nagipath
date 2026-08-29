package web

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// These tests used to skip themselves on an empty database — twelve of them,
// asserting on an empty page and passing. A site screen is only worth checking
// over a site, so both tests here seed the nginx fixture first: two hosts with
// byte-identical configuration serving shop.example.com, one upstream pool with
// one member, one route that proxies and one that serves files from disk.

// sitesFixture is the two-node fleet both tests read, signed in as a viewer:
// every panel below is read-only, so the lower role is the honest one to use.
func sitesFixture(t *testing.T) *client {
	t.Helper()
	s, db := newTestServer(t)
	if _, err := db.CreateUser(t.Context(), "viewer", "a good long password", "viewer", "Viewer", false); err != nil {
		t.Fatal(err)
	}
	seedNginx(t, db, "web02", "10.90.4.11")
	seedNginx(t, db, "web05", "10.90.4.12")
	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"viewer"}, "password": {"a good long password"}})
	if c.cookie == "" {
		t.Fatal("the viewer could not sign in")
	}
	return c
}

type apiSiteRow struct {
	Name  string `json:"Name"`
	Nodes int    `json:"Nodes"`
	State string `json:"State"`
}

type apiSitesList struct {
	Rows     []apiSiteRow `json:"Rows"`
	Sel      string       `json:"Sel"`
	Variants []any        `json:"Variants"`
}

// The fleet-wide list answers one question per column, and the numbers in them
// have to be the fleet's: two nodes serve this hostname, and they agree. Sites
// is the React SPA now (docs/adr/0017), so what used to be HTML substring
// checks are field assertions against GET /api/ui/sites.
func TestSitesListCarriesTheFleetsNumbers(t *testing.T) {
	c := sitesFixture(t)

	w := c.get("/api/ui/sites")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/ui/sites = %d\n%s", w.Code, w.Body.String())
	}
	var got apiSitesList
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode /api/ui/sites: %v", err)
	}
	if len(got.Rows) != 1 || got.Rows[0].Name != "shop.example.com" || got.Rows[0].Nodes != 2 {
		t.Fatalf("Rows = %+v, want one row for shop.example.com with 2 nodes", got.Rows)
	}

	// The filter is in the URL and it filters. A query matching nothing has to say
	// so rather than fall back to the unfiltered list.
	var filtered apiSitesList
	if err := json.Unmarshal(c.get("/api/ui/sites?q=shop").Body.Bytes(), &filtered); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(filtered.Rows) != 1 {
		t.Error("/api/ui/sites?q=shop dropped the row it matches")
	}
	var miss apiSitesList
	if err := json.Unmarshal(c.get("/api/ui/sites?q=nothing-serves-this").Body.Bytes(), &miss); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(miss.Rows) != 0 {
		t.Error("/api/ui/sites?q= with no match returned rows anyway")
	}

	// The selected-site panel reads Variants, and a ?site= naming a hostname
	// nothing serves has a selection and no variants — this used to panic in the
	// Go template on an empty slice; the API's job now is just not to 500.
	w = c.get("/api/ui/sites?site=nothing-serves-this")
	if w.Code != http.StatusOK {
		t.Errorf("GET /api/ui/sites?site= naming an unserved hostname = %d, want 200", w.Code)
	}
}

// Each tab of the site page claims different facts, and a tab is only checked by
// the fact it claims: the fields these branches read are resolved when the
// branch runs, so a rename in the store reaches a client as a 500 otherwise.
func TestSiteDetailTabsCarryTheirOwnFacts(t *testing.T) {
	c := sitesFixture(t)

	// Overview: the route table and the "nodes serving this site" panel.
	w := c.get("/api/ui/sites/shop.example.com")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/ui/sites/shop.example.com = %d\n%s", w.Code, w.Body.String())
	}
	var overview struct {
		Overview *struct {
			Routes []struct {
				Pattern string `json:"Pattern"`
			} `json:"Routes"`
		} `json:"Overview"`
		Nodes []struct {
			NodeName string `json:"NodeName"`
		} `json:"Nodes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &overview); err != nil {
		t.Fatalf("decode overview: %v", err)
	}
	if overview.Overview == nil {
		t.Fatal("overview tab returned no Overview")
	}
	foundRoute := false
	for _, r := range overview.Overview.Routes {
		if strings.Contains(r.Pattern, "/api/") {
			foundRoute = true
		}
	}
	if !foundRoute {
		t.Errorf("overview routes = %+v, want one matching /api/", overview.Overview.Routes)
	}
	foundNode := false
	for _, n := range overview.Nodes {
		if n.NodeName == "web05" {
			foundNode = true
		}
	}
	if !foundNode {
		t.Errorf("overview nodes = %+v, want web05 among them", overview.Nodes)
	}

	// Nodes tab.
	var nodesTab struct {
		Nodes []struct {
			NodeName string `json:"NodeName"`
		} `json:"Nodes"`
	}
	if err := json.Unmarshal(c.get("/api/ui/sites/shop.example.com?tab=nodes").Body.Bytes(), &nodesTab); err != nil {
		t.Fatalf("decode nodes tab: %v", err)
	}
	found := false
	for _, n := range nodesTab.Nodes {
		if n.NodeName == "web02" {
			found = true
		}
	}
	if !found {
		t.Errorf("nodes tab = %+v, want web02 among them", nodesTab.Nodes)
	}

	// Upstreams tab: the pool member behind the proxy route.
	var upstreamsTab struct {
		Upstreams []struct {
			Port int `json:"Port"`
		} `json:"Upstreams"`
	}
	if err := json.Unmarshal(c.get("/api/ui/sites/shop.example.com?tab=upstreams").Body.Bytes(), &upstreamsTab); err != nil {
		t.Fatalf("decode upstreams tab: %v", err)
	}
	found = false
	for _, u := range upstreamsTab.Upstreams {
		if u.Port == 8080 {
			found = true
		}
	}
	if !found {
		t.Errorf("upstreams tab = %+v, want a member on port 8080", upstreamsTab.Upstreams)
	}

	// Certificates tab: the binding panel, empty or not, must be present (not nil).
	var certsTab struct {
		Certs []any `json:"Certs"`
	}
	body := c.get("/api/ui/sites/shop.example.com?tab=certificates").Body.Bytes()
	if err := json.Unmarshal(body, &certsTab); err != nil {
		t.Fatalf("decode certificates tab: %v", err)
	}

	// The variant filter keeps the site.
	if got := c.get("/api/ui/sites/shop.example.com?variant=A").Code; got != http.StatusOK {
		t.Errorf("GET .../shop.example.com?variant=A = %d, want 200", got)
	}

	// A hostname no node serves is a 404, not an empty page pretending the site
	// exists. This used to be a 404 for every site in the fleet because the handler
	// folded its query error into notFound.
	if got := c.get("/api/ui/sites/nothing-serves-this").Code; got != http.StatusNotFound {
		t.Errorf("GET /api/ui/sites/nothing-serves-this = %d, want 404", got)
	}
}
