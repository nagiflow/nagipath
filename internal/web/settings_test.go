package web

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

// The store-level assertions that used to live here — host key stats, master key
// counts, retention stats, token prefix — are in store/settings_view_test.go,
// which is where they belong: none of them touched the server. What is left is
// the part only a request can check, which is that each screen renders and that
// its state filter is a real URL a person can paste into a ticket.

func TestSettingsIndex(t *testing.T) {
	s, db := newTestServer(t)
	defer db.Close()

	ctx := context.Background()
	db.CreateUser(ctx, "admin", "password123456", "admin", "Admin", false)
	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"password123456"}})

	body := c.get("/settings").Body.String()
	for _, want := range []string{"Users & roles", "Retention", "Collection defaults"} {
		if !strings.Contains(body, want) {
			t.Errorf("settings index is missing the %q panel", want)
		}
	}
}

func TestHostKeysStateFilter(t *testing.T) {
	s, db := newTestServer(t)
	defer db.Close()
	ctx := context.Background()

	db.CreateUser(ctx, "admin", "password123456", "admin", "Admin", false)
	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"password123456"}})

	nodeID, err := db.AddNode(ctx, "192.0.2.1", 22, "test-node", "nagipath", nil, nil, "manual", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = db.W.ExecContext(ctx, `INSERT INTO host_key
		(node_id, algorithm, public_key, fingerprint, state, first_seen_at)
		VALUES (?, 'ed25519', 'key1', 'SHA256:pending', 'pending', datetime('now'))`, nodeID)
	_, _ = db.W.ExecContext(ctx, `INSERT INTO host_key
		(node_id, algorithm, public_key, fingerprint, state, first_seen_at, decided_at)
		VALUES (?, 'rsa', 'key2', 'SHA256:approved', 'approved', datetime('now'), datetime('now'))`, nodeID)

	// Each filter is its own URL, and each has to name the key it kept — a filter
	// that 200s while rendering the unfiltered table would pass a status check.
	for path, want := range map[string]string{
		"/settings/hostkeys?state=pending":  "SHA256:pending",
		"/settings/hostkeys?state=approved": "SHA256:approved",
	} {
		w := c.get(path)
		if w.Code != 200 {
			t.Errorf("GET %s = %d", path, w.Code)
			continue
		}
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("GET %s did not show %s", path, want)
		}
	}
}

func TestAPITokenStateFilter(t *testing.T) {
	s, db := newTestServer(t)
	defer db.Close()
	ctx := context.Background()

	adminID, err := db.CreateUser(ctx, "admin", "password123456", "admin", "Admin", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.CreateAPIToken(ctx, "active-token", adminID, 0); err != nil {
		t.Fatal(err)
	}
	revokedID, _, err := db.CreateAPIToken(ctx, "revoked-token", adminID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.RevokeAPIToken(ctx, revokedID); err != nil {
		t.Fatal(err)
	}

	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"password123456"}})

	for path, want := range map[string]string{
		"/settings/api-keys?state=active":  "active-token",
		"/settings/api-keys?state=revoked": "revoked-token",
	} {
		w := c.get(path)
		if w.Code != 200 {
			t.Errorf("GET %s = %d", path, w.Code)
			continue
		}
		body := w.Body.String()
		if !strings.Contains(body, want) {
			t.Errorf("GET %s did not show %s", path, want)
		}
		// The other token must be gone, or the filter is decorative.
		other := "revoked-token"
		if want == other {
			other = "active-token"
		}
		if strings.Contains(body, other) {
			t.Errorf("GET %s still shows %s", path, other)
		}
	}
}
