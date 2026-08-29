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
