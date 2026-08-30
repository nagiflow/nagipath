package web

import (
	"net/http"
	"testing"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

// TestAPISessionEndpoint exercises the one thing Phase 0 of the SPA rewrite
// (docs/adr/0017) needs proven: a request carrying the same session cookie
// and CSRF token the server-rendered pages use reaches internal/api through
// internal/web's mux and gets a real session payload back. Since Phase 8,
// GET /api/session is public rather than 401ing an anonymous caller — the
// Login/Setup pages need to know authenticated/setup_required before any
// session exists — so this is a thin plumbing smoke test; the exhaustive
// login/setup/password business logic lives in internal/api/auth_test.go.
// Decoded via protojson against the generated schema (docs/adr/0018), not
// hand-typed json tags.
func TestAPISessionEndpoint(t *testing.T) {
	s, _ := newTestServer(t)
	c := &client{t: t, s: s}

	w := c.get("/api/session")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/session with no session = %d, want 200", w.Code)
	}
	var anon pb.SessionResponse
	if err := protojson.Unmarshal(w.Body.Bytes(), &anon); err != nil {
		t.Fatalf("decode session response: %v (body %q)", err, w.Body.String())
	}
	if anon.Authenticated || !anon.SetupRequired {
		t.Errorf("anonymous session on an empty database = %+v, want authenticated=false setup_required=true", &anon)
	}

	c.bootstrapAdmin(t, "admin", "a good long password")

	w = c.get("/api/session")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/session after setup = %d, body %q", w.Code, w.Body.String())
	}
	var resp pb.SessionResponse
	if err := protojson.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode session response: %v (body %q)", err, w.Body.String())
	}
	if !resp.Authenticated {
		t.Error("session after setup should be authenticated")
	}
	if resp.User.Username != "admin" || resp.User.Role != "admin" {
		t.Errorf("user = %+v, want username=admin role=admin", resp.User)
	}
	if resp.CsrfToken != c.csrf {
		t.Errorf("csrf_token = %q, want the same token the cookie-based flow uses (%q)", resp.CsrfToken, c.csrf)
	}
	if resp.NavCounts.Nodes != 0 {
		t.Errorf("nav_counts.nodes = %d on an empty fleet, want 0", resp.NavCounts.Nodes)
	}
}
