package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestDashboardServesSPAShell confirms "/" is the React SPA now (docs/adr/0017):
// still gated by s.auth() like every other route, but the body it returns is
// the built index.html, not a rendered dashboard.html.
func TestDashboardServesSPAShell(t *testing.T) {
	s, _ := newTestServer(t)
	c := &client{t: t, s: s}

	// Unauthenticated still redirects to /setup, same as any other route.
	if got := c.get("/").Code; got != http.StatusSeeOther {
		t.Fatalf("GET / with no users = %d, want a redirect", got)
	}

	c.post("/setup", url.Values{
		"username": {"admin"}, "password": {"a good long password"},
		"confirm": {"a good long password"},
	})

	w := c.get("/")
	if w.Code != http.StatusOK {
		t.Fatalf("GET / after setup = %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `id="root"`) || !strings.Contains(body, "/assets/") {
		t.Errorf("GET / did not return the SPA shell:\n%s", body)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("GET / Content-Type = %q, want text/html", ct)
	}

	// The SPA's bundle itself must resolve through the same embed.
	if got := c.get("/favicon.svg").Code; got != http.StatusOK {
		t.Errorf("GET /favicon.svg = %d, want 200", got)
	}
}

// TestClustersServesSPAShell mirrors TestDashboardServesSPAShell for the
// second route cut over to the SPA (docs/adr/0017, Phase 2).
func TestClustersServesSPAShell(t *testing.T) {
	s, _ := newTestServer(t)
	c := &client{t: t, s: s}
	c.post("/setup", url.Values{
		"username": {"admin"}, "password": {"a good long password"},
		"confirm": {"a good long password"},
	})

	w := c.get("/clusters")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /clusters = %d", w.Code)
	}
	if body := w.Body.String(); !strings.Contains(body, `id="root"`) {
		t.Errorf("GET /clusters did not return the SPA shell:\n%s", body)
	}
}

// TestSitesServesSPAShell mirrors TestDashboardServesSPAShell for the Sites
// routes cut over to the SPA (docs/adr/0017, Phase 2).
func TestSitesServesSPAShell(t *testing.T) {
	s, _ := newTestServer(t)
	c := &client{t: t, s: s}
	c.post("/setup", url.Values{
		"username": {"admin"}, "password": {"a good long password"},
		"confirm": {"a good long password"},
	})

	for _, path := range []string{"/sites", "/sites/shop.example.com"} {
		w := c.get(path)
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s = %d", path, w.Code)
		}
		if body := w.Body.String(); !strings.Contains(body, `id="root"`) {
			t.Errorf("GET %s did not return the SPA shell:\n%s", path, body)
		}
	}
}

// TestNodesServesSPAShell mirrors TestDashboardServesSPAShell for the Nodes
// routes cut over to the SPA (docs/adr/0017, Phase 2). /nodes/{id} always
// 200s at this level, even for an id that doesn't exist — that check moved
// to NodeDetailPage's client-side handling of the API's 404 (see
// web_test.go's TestNotFoundIsAPageInsideTheShell).
func TestNodesServesSPAShell(t *testing.T) {
	s, _ := newTestServer(t)
	c := &client{t: t, s: s}
	c.post("/setup", url.Values{
		"username": {"admin"}, "password": {"a good long password"},
		"confirm": {"a good long password"},
	})

	for _, path := range []string{"/nodes", "/nodes/4242"} {
		w := c.get(path)
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s = %d", path, w.Code)
		}
		if body := w.Body.String(); !strings.Contains(body, `id="root"`) {
			t.Errorf("GET %s did not return the SPA shell:\n%s", path, body)
		}
	}
}

// TestAnalysisServesSPAShell mirrors TestDashboardServesSPAShell for the
// Analysis group (Drift, Certificates, Snapshots) cut over to the SPA
// (docs/adr/0017, Phase 3).
func TestAnalysisServesSPAShell(t *testing.T) {
	s, _ := newTestServer(t)
	c := &client{t: t, s: s}
	c.post("/setup", url.Values{
		"username": {"admin"}, "password": {"a good long password"},
		"confirm": {"a good long password"},
	})

	for _, path := range []string{
		"/drift", "/drift/review/4242",
		"/certificates", "/certificates/4242",
		"/snapshots", "/snapshots/4242/file/1",
	} {
		w := c.get(path)
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s = %d", path, w.Code)
		}
		if body := w.Body.String(); !strings.Contains(body, `id="root"`) {
			t.Errorf("GET %s did not return the SPA shell:\n%s", path, body)
		}
	}
}

// TestSettingsServesSPAShell mirrors TestDashboardServesSPAShell for the
// Settings and Collections group cut over to the SPA (docs/adr/0017, Phase 4).
func TestSettingsServesSPAShell(t *testing.T) {
	s, _ := newTestServer(t)
	c := &client{t: t, s: s}
	c.post("/setup", url.Values{
		"username": {"admin"}, "password": {"a good long password"},
		"confirm": {"a good long password"},
	})

	for _, path := range []string{
		"/collections", "/settings", "/settings/credentials", "/settings/hostkeys",
		"/settings/masterkey", "/settings/collection-defaults", "/settings/retention",
		"/settings/users", "/settings/api-keys", "/settings/audit", "/settings/license",
		"/settings/system",
	} {
		w := c.get(path)
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s = %d", path, w.Code)
		}
		if body := w.Body.String(); !strings.Contains(body, `id="root"`) {
			t.Errorf("GET %s did not return the SPA shell:\n%s", path, body)
		}
	}
}

// TestExploreServesSPAShell mirrors TestDashboardServesSPAShell for Rule
// lookup, Config search, Trace and Probe history, cut over to the SPA
// (docs/adr/0017, Phase 5 and Phase 6).
func TestExploreServesSPAShell(t *testing.T) {
	s, _ := newTestServer(t)
	c := &client{t: t, s: s}
	c.post("/setup", url.Values{
		"username": {"admin"}, "password": {"a good long password"},
		"confirm": {"a good long password"},
	})

	for _, path := range []string{
		"/rules", "/rules?hostname=shop.example.com&path=/api",
		"/search", "/search?q=proxy_pass",
		"/trace", "/trace?url=shop.example.com/api/v2", "/trace/probe",
		"/trace/history",
	} {
		w := c.get(path)
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s = %d", path, w.Code)
		}
		if body := w.Body.String(); !strings.Contains(body, `id="root"`) {
			t.Errorf("GET %s did not return the SPA shell:\n%s", path, body)
		}
	}
}
