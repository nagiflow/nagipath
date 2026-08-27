package web

import (
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

// The fleet-wide list answers one question per column, and the numbers in them
// have to be the fleet's: two nodes serve this hostname, and they agree.
func TestSitesListCarriesTheFleetsNumbers(t *testing.T) {
	c := sitesFixture(t)

	w := c.get("/sites")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /sites = %d\n%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{
		"Hostname", "Nodes", "Listener", "Certificate", "Routes", "Config", "State",
		"shop.example.com", // the hostname the fixture serves
		"IDENTICAL",        // both nodes hold the same bytes, so there is one variant
	} {
		if !strings.Contains(body, want) {
			t.Errorf("/sites is missing %q", want)
		}
	}
	if strings.Contains(body, "No sites match this filter") {
		t.Error("/sites rendered its empty state over a collected fleet")
	}

	// The filter is in the URL and it filters. A query matching nothing has to say
	// so rather than fall back to the unfiltered list.
	if body := c.get("/sites?q=shop").Body.String(); !strings.Contains(body, "shop.example.com") {
		t.Error("/sites?q=shop dropped the row it matches")
	}
	miss := c.get("/sites?q=nothing-serves-this").Body.String()
	if strings.Contains(miss, "shop.example.com") || !strings.Contains(miss, "No sites match this filter") {
		t.Error("/sites?q= with no match did not render the empty state")
	}

	// The selected-site panel reads .Variants, and a ?site= naming a hostname
	// nothing serves has a selection and no variants — `index .Variants 0` on the
	// empty slice panicked rather than rendering an empty panel.
	if got := c.get("/sites?site=nothing-serves-this").Code; got != http.StatusOK {
		t.Errorf("GET /sites?site= naming an unserved hostname = %d, want 200", got)
	}

	// The hostname has to reach the site's own page. It pointed at ?site=, which
	// reloads this same list, so there was no path to the detail screen short of a
	// small button inside the preview strip — the screen read as missing.
	if !strings.Contains(body, `href="/sites/shop.example.com"`) {
		t.Error("/sites does not link its hostnames to the site detail page")
	}
}

// Each tab of the site page claims different facts, and a tab is only checked by
// the fact it claims: the fields these branches read are resolved when the
// branch runs, so a rename in the store reaches a browser as a 500 otherwise.
func TestSiteDetailTabsCarryTheirOwnFacts(t *testing.T) {
	c := sitesFixture(t)

	for _, want := range []struct{ path, text string }{
		{"/sites/shop.example.com", "/api/"},                               // overview lists the routes in match order
		{"/sites/shop.example.com", "Nodes serving this site"},             // …and which hosts serve them, per the design
		{"/sites/shop.example.com", "web05"},                               // that panel is filled, not just titled
		{"/sites/shop.example.com?tab=nodes", "web02"},                     // which hosts serve it
		{"/sites/shop.example.com?tab=upstreams", "8080"},                  // the pool member behind the proxy route
		{"/sites/shop.example.com?tab=certificates", "Certificates bound"}, // the binding panel, empty or not
		{"/sites/shop.example.com?variant=A", "shop.example.com"},          // the variant filter keeps the site
	} {
		w := c.get(want.path)
		if w.Code != http.StatusOK {
			t.Errorf("GET %s = %d\n%s", want.path, w.Code, w.Body.String())
			continue
		}
		body := w.Body.String()
		if !strings.Contains(body, want.text) {
			t.Errorf("GET %s is missing %q", want.path, want.text)
		}
		if !strings.Contains(body, "</html>") {
			t.Errorf("GET %s rendered a truncated page (template error mid-render)", want.path)
		}
	}

	// A hostname no node serves is a 404, not an empty page pretending the site
	// exists. This used to be a 404 for every site in the fleet because the handler
	// folded its query error into notFound.
	if got := c.get("/sites/nothing-serves-this").Code; got != http.StatusNotFound {
		t.Errorf("GET /sites/nothing-serves-this = %d, want 404", got)
	}
}
