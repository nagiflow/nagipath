package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

// GET /api/ui/settings/system behaves like every other JSON route behind
// requireAuth: signed out, it 401s rather than returning anything.
func TestDiagnosticsPageRequiresAuth(t *testing.T) {
	s, db := newTestServer(t)
	db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false)
	c := &client{t: t, s: s}
	w := c.get("/api/ui/settings/system")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("GET /api/ui/settings/system while signed out = %d, want 401", w.Code)
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

	if got := c.get("/api/ui/settings/system").Code; got != http.StatusForbidden {
		t.Errorf("GET /api/ui/settings/system as a viewer = %d, want 403", got)
	}
	if got := c.get("/settings/system/bundle").Code; got != http.StatusForbidden {
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

	var resp pb.DiagnosticsResponse
	w := c.get("/api/ui/settings/system")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/ui/settings/system as an admin = %d, want 200: %s", w.Code, w.Body.String())
	}
	if err := protojson.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.DbPath != s.DB.Path || resp.MigrationsApplied == 0 {
		t.Errorf("diagnostics response missing expected fields: dbPath=%q migrationsApplied=%d",
			resp.DbPath, resp.MigrationsApplied)
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

	w := c.get("/settings/system/bundle")
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
