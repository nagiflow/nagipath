package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// GET /license behaves like every other page behind auth(): signed out, it
// redirects to /login rather than rendering anything.
func TestLicensePageRequiresAuth(t *testing.T) {
	s, db := newTestServer(t)
	// An admin has to exist, or auth() redirects to /setup instead of /login —
	// this test is about the latter.
	db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false)
	c := &client{t: t, s: s}
	w := c.get("/license")
	if w.Code != http.StatusSeeOther || !strings.HasPrefix(w.Header().Get("Location"), "/login") {
		t.Errorf("GET /license while signed out = %d -> %q", w.Code, w.Header().Get("Location"))
	}
}

// A viewer can read the page (both roles can view license status per the
// design doc) but must not be handed the install form — that action is
// admin-only, and the route itself refuses a viewer's POST regardless.
func TestViewerCanViewLicenseButNotInstall(t *testing.T) {
	s, db := newTestServer(t)
	db.CreateUser(t.Context(), "viewer", "a good long password", "viewer", "Viewer", false)
	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"viewer"}, "password": {"a good long password"}})
	if c.cookie == "" {
		t.Fatal("viewer could not sign in")
	}

	w := c.get("/license")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /license as a viewer = %d, want 200: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "</html>") {
		t.Fatal("GET /license rendered a truncated page (template error mid-render)")
	}
	if strings.Contains(body, "license_text") {
		t.Error("a viewer's /license page rendered the install form, which is admin-only")
	}

	if got := c.post("/license", url.Values{"license_text": {"whatever"}}).Code; got != http.StatusForbidden {
		t.Errorf("POST /license as a viewer = %d, want 403", got)
	}
}

// A malformed paste must be rejected without touching the currently loaded
// License or writing a license_state row — a bad paste is a no-op, not a
// partial state change.
func TestInstallingAGarbledLicenseChangesNothing(t *testing.T) {
	s, db := newTestServer(t)
	db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false)
	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	w := c.post("/license", url.Values{"license_text": {"this is not a license file"}})
	if loc := w.Header().Get("Location"); !strings.Contains(loc, "err=") {
		t.Errorf("installing garbage license text was not rejected (redirect %q)", loc)
	}
	if s.currentLicense() != nil {
		t.Error("s.currentLicense() changed despite the install failing")
	}
	st, err := db.LicenseState(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if st != nil {
		t.Errorf("a license_state row was written despite the install failing: %+v", st)
	}
}

// license.PublicKey's matching private key is deliberately confined to
// internal/license/license_test.go and must never be exposed to this
// package, so a validly-signed install cannot be exercised here. Instead,
// seed license_state directly the way a real install would leave it, and
// confirm the page shows what was seeded.
func TestLicensePageRendersSeededLicenseState(t *testing.T) {
	s, db := newTestServer(t)
	db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false)
	adminUsers, err := db.Users(t.Context())
	if err != nil || len(adminUsers) != 1 {
		t.Fatalf("Users() = %v, %v", adminUsers, err)
	}
	adminID := adminUsers[0].ID

	expiry := time.Date(2030, 6, 30, 0, 0, 0, 0, time.UTC)
	if err := db.UpsertLicenseState(t.Context(), "raw-blob", "Acme Corp", "enterprise", 200, expiry, true, &adminID); err != nil {
		t.Fatal(err)
	}

	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})
	body := c.get("/license").Body.String()
	for _, want := range []string{"Acme Corp", "enterprise", "2030-06-30", "admin"} {
		if !strings.Contains(body, want) {
			t.Errorf("GET /license is missing %q\n%s", want, body)
		}
	}
}
