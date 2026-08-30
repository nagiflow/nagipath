package web

import (
	"net/http"
	"strings"
	"testing"
	"time"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

// GET /api/settings/license behaves like every other JSON route behind
// requireAuth: signed out, it 401s rather than returning anything.
func TestLicensePageRequiresAuth(t *testing.T) {
	s, db := newTestServer(t)
	// An admin has to exist, or auth() redirects to /setup instead of /login —
	// this test is about the latter.
	db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false)
	c := &client{t: t, s: s}
	w := c.get("/api/settings/license")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("GET /api/settings/license while signed out = %d, want 401", w.Code)
	}
}

// A viewer can read license status (both roles can, per the design doc) but
// must not be able to install one — that action is admin-only.
func TestViewerCanViewLicenseButNotInstall(t *testing.T) {
	s, db := newTestServer(t)
	db.CreateUser(t.Context(), "viewer", "a good long password", "viewer", "Viewer", false)
	c := &client{t: t, s: s}
	c.login("viewer", "a good long password")
	if c.cookie == "" {
		t.Fatal("viewer could not sign in")
	}

	w := c.get("/api/settings/license")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/settings/license as a viewer = %d, want 200: %s", w.Code, w.Body.String())
	}

	if got := c.postJSON("/api/settings/license", map[string]any{"licenseText": "whatever"}).Code; got != http.StatusForbidden {
		t.Errorf("POST /api/settings/license as a viewer = %d, want 403", got)
	}
}

// A malformed paste must be rejected without touching the currently loaded
// License or writing a license_state row — a bad paste is a no-op, not a
// partial state change.
func TestInstallingAGarbledLicenseChangesNothing(t *testing.T) {
	s, db := newTestServer(t)
	db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false)
	c := &client{t: t, s: s}
	c.login("admin", "a good long password")

	w := c.postJSON("/api/settings/license", map[string]any{"licenseText": "this is not a license file"})
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("installing garbage license text was not rejected: %d %s", w.Code, w.Body.String())
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
	c.login("admin", "a good long password")
	var resp pb.LicenseResponse
	if err := protojson.Unmarshal(c.get("/api/settings/license").Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.State == nil {
		t.Fatal("license response has no state")
	}
	if resp.State.Customer != "Acme Corp" || resp.State.Edition != "enterprise" ||
		resp.State.InstalledByUsername != "admin" || !strings.HasPrefix(resp.State.ExpiresAt, "2030-06-30") {
		t.Errorf("GET /api/settings/license state = %+v", resp.State)
	}
}
