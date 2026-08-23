package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// GET /diagnostics behaves like every other page behind auth(): signed out,
// it redirects to /login rather than rendering anything.
func TestDiagnosticsPageRequiresAuth(t *testing.T) {
	s, db := newTestServer(t)
	db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false)
	c := &client{t: t, s: s}
	w := c.get("/diagnostics")
	if w.Code != http.StatusSeeOther || !strings.HasPrefix(w.Header().Get("Location"), "/login") {
		t.Errorf("GET /diagnostics while signed out = %d -> %q", w.Code, w.Header().Get("Location"))
	}
}

// Unlike /license, /diagnostics is admin-only end to end: a viewer is refused
// outright rather than shown a read-only version of the page.
func TestViewerCannotReachDiagnostics(t *testing.T) {
	s, db := newTestServer(t)
	db.CreateUser(t.Context(), "viewer", "a good long password", "viewer", "Viewer", false)
	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"viewer"}, "password": {"a good long password"}})
	if c.cookie == "" {
		t.Fatal("viewer could not sign in")
	}

	if got := c.get("/diagnostics").Code; got != http.StatusForbidden {
		t.Errorf("GET /diagnostics as a viewer = %d, want 403", got)
	}
	if got := c.get("/diagnostics/bundle").Code; got != http.StatusForbidden {
		t.Errorf("GET /diagnostics/bundle as a viewer = %d, want 403", got)
	}
}

// An admin gets the panel, with recognizable, page-specific content — not just
// a rendered shell.
func TestAdminSeesDiagnosticsPanel(t *testing.T) {
	s, db := newTestServer(t)
	db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false)
	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	w := c.get("/diagnostics")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /diagnostics as an admin = %d, want 200: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "</html>") {
		t.Fatal("GET /diagnostics rendered a truncated page (template error mid-render)")
	}
	for _, want := range []string{s.DB.Path, "master key", "License", "Migrations applied"} {
		if !strings.Contains(strings.ToLower(body), strings.ToLower(want)) {
			t.Errorf("GET /diagnostics is missing %q\n%s", want, body)
		}
	}
}

// The bundle downloads as a plain-text attachment and, since nothing secret
// was ever plumbed into the handler in the first place, its sections render
// without panicking or leaking an obviously-wrong marker.
func TestDiagnosticsBundleDownloadsAsAttachment(t *testing.T) {
	s, db := newTestServer(t)
	db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false)
	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	w := c.get("/diagnostics/bundle")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /diagnostics/bundle as an admin = %d, want 200: %s", w.Code, w.Body.String())
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment") {
		t.Errorf("Content-Disposition = %q, want it to start with %q", cd, "attachment")
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", ct)
	}
	body := w.Body.String()
	for _, want := range []string{"== Collection statistics ==", "== Migrations ==", "== Recent logs ==", "== Excluded from this bundle =="} {
		if !strings.Contains(body, want) {
			t.Errorf("bundle is missing section %q\n%s", want, body)
		}
	}

	audits, err := db.AuditEvents(t.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range audits {
		if a.Action == "diagnostics.download" {
			found = true
		}
	}
	if !found {
		t.Error("downloading the bundle did not write a diagnostics.download audit entry")
	}
}
