package web

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
)

// TestAPISessionEndpoint exercises the one thing Phase 0 of the SPA rewrite
// (docs/adr/0017) needs proven: a request carrying the same session cookie
// and CSRF token the server-rendered pages use reaches internal/api through
// internal/web's mux and gets a real session payload back, and a request with
// neither does not.
func TestAPISessionEndpoint(t *testing.T) {
	s, _ := newTestServer(t)
	c := &client{t: t, s: s}

	if got := c.get("/api/ui/session").Code; got != http.StatusUnauthorized {
		t.Fatalf("GET /api/ui/session with no session = %d, want 401", got)
	}

	c.post("/setup", url.Values{
		"username": {"admin"}, "password": {"a good long password"},
		"confirm": {"a good long password"},
	})
	if c.cookie == "" {
		t.Fatal("setup did not establish a session")
	}

	w := c.get("/api/ui/session")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/ui/session after setup = %d, body %q", w.Code, w.Body.String())
	}
	var resp struct {
		User struct {
			Username string `json:"username"`
			Role     string `json:"role"`
		} `json:"user"`
		CSRFToken string `json:"csrf_token"`
		NavCounts struct {
			Nodes int `json:"nodes"`
		} `json:"nav_counts"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode session response: %v (body %q)", err, w.Body.String())
	}
	if resp.User.Username != "admin" || resp.User.Role != "admin" {
		t.Errorf("user = %+v, want username=admin role=admin", resp.User)
	}
	if resp.CSRFToken != c.csrf {
		t.Errorf("csrf_token = %q, want the same token the cookie-based flow uses (%q)", resp.CSRFToken, c.csrf)
	}
	if resp.NavCounts.Nodes != 0 {
		t.Errorf("nav_counts.nodes = %d on an empty fleet, want 0", resp.NavCounts.Nodes)
	}
}
