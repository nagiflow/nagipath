package web

import (
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/nagiflow/nagipath/internal/store"
)

// addNode and seedPendingKey exist because these fixtures used to discard their
// errors. `source: "test"` violates the source CHECK, so every node insert
// failed silently and the screen under test was rendering an empty fleet — the
// assertions were failing on the right screen with the wrong database.
func addNode(t *testing.T, db *store.DB, addr, name string, by int64) int64 {
	t.Helper()
	id, err := db.AddNode(t.Context(), addr, 22, name, "nagipath", nil, nil, "manual", &by)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func seedPendingKey(t *testing.T, s *Server, fingerprint, pubKey, nodeName string) {
	t.Helper()
	res, err := s.DB.W.ExecContext(t.Context(), `INSERT INTO host_key
		(node_id, fingerprint, algorithm, public_key, state, first_seen_at)
		SELECT id, ?, ?, ?, 'pending', ? FROM node WHERE display_name = ?`,
		fingerprint, "ed25519", pubKey, store.Now(), nodeName)
	if err != nil {
		t.Fatal(err)
	}
	// A SELECT-driven INSERT matching no node inserts nothing and reports no error,
	// which is the same silent-empty-fixture failure by another route.
	if n, _ := res.RowsAffected(); n == 0 {
		t.Fatalf("no node named %q to hang a host key on", nodeName)
	}
}

// TestOnboardingHostKeyGrouping verifies that pending host keys are grouped by
// shared fingerprint, so approving 38 nodes presenting one image-built key is
// one decision rather than 38.
func TestOnboardingHostKeyGrouping(t *testing.T) {
	pending := []store.PendingHostKey{
		{HostKey: store.HostKey{ID: 1, Fingerprint: "SHA256:abc123", Algorithm: "ed25519"}, NodeName: "web01"},
		{HostKey: store.HostKey{ID: 2, Fingerprint: "SHA256:abc123", Algorithm: "ed25519"}, NodeName: "web02"},
		{HostKey: store.HostKey{ID: 3, Fingerprint: "SHA256:abc123", Algorithm: "ed25519"}, NodeName: "web03"},
		{HostKey: store.HostKey{ID: 4, Fingerprint: "SHA256:def456", Algorithm: "ed25519"}, NodeName: "lb01"},
		{HostKey: store.HostKey{ID: 5, Fingerprint: "SHA256:ghi789", Algorithm: "rsa"}, NodeName: "app01"},
	}

	groups := groupKeysByFingerprint(pending)

	if len(groups) != 3 {
		t.Fatalf("got %d groups, want 3", len(groups))
	}

	// First group: 3 web nodes sharing one fingerprint
	if len(groups[0].Keys) != 3 {
		t.Errorf("group 0: got %d keys, want 3", len(groups[0].Keys))
	}
	if groups[0].Fingerprint != "SHA256:abc123" {
		t.Errorf("group 0: fingerprint %q, want SHA256:abc123", groups[0].Fingerprint)
	}
	if groups[0].Algorithm != "ed25519" {
		t.Errorf("group 0: algorithm %q, want ed25519", groups[0].Algorithm)
	}

	// Second group: 1 lb node
	if len(groups[1].Keys) != 1 {
		t.Errorf("group 1: got %d keys, want 1", len(groups[1].Keys))
	}
	if groups[1].Fingerprint != "SHA256:def456" {
		t.Errorf("group 1: fingerprint %q, want SHA256:def456", groups[1].Fingerprint)
	}

	// Third group: 1 app node
	if len(groups[2].Keys) != 1 {
		t.Errorf("group 2: got %d keys, want 1", len(groups[2].Keys))
	}
	if groups[2].Fingerprint != "SHA256:ghi789" {
		t.Errorf("group 2: fingerprint %q, want SHA256:ghi789", groups[2].Fingerprint)
	}
	if groups[2].Algorithm != "rsa" {
		t.Errorf("group 2: algorithm %q, want rsa", groups[2].Algorithm)
	}
}

// TestOnboardingRenderGroupedKeys verifies the onboarding screen renders
// grouped host keys correctly.
func TestOnboardingRenderGroupedKeys(t *testing.T) {
	s, db := newTestServer(t)
	ctx := t.Context()

	// Create admin and sign in
	db.CreateUser(ctx, "admin", "a good long password", "admin", "Admin", false)
	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	// Add nodes
	adminID := int64(1)
	for i := 1; i <= 3; i++ {
		addNode(t, db, fmt.Sprintf("192.0.2.%d", i), fmt.Sprintf("web0%d", i), adminID)
	}
	addNode(t, db, "192.0.2.10", "lb01", adminID)

	// Add pending host keys: 3 share one fingerprint, 1 unique
	sharedFP := "SHA256:abc123def456"
	sharedPubKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIshared"
	for i := 1; i <= 3; i++ {
		seedPendingKey(t, s, sharedFP, sharedPubKey, fmt.Sprintf("web0%d", i))
	}
	seedPendingKey(t, s, "SHA256:unique789", "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIunique", "lb01")

	// Add a credential so we reach step 3 (host keys). Through the real API, sealed
	// with the real master key: a hand-written INSERT here was already one schema
	// change out of date and failing silently.
	if _, err := db.CreateCredential(ctx, s.Master, "test", "nagipath", "private_key", testPrivateKey, "", "", &adminID); err != nil {
		t.Fatal(err)
	}

	w := c.get("/onboarding")
	if w.Code != 200 {
		t.Fatalf("GET /onboarding = %d, want 200", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "</html>") {
		t.Fatal("onboarding page rendered truncated (template error mid-render)")
	}

	// Should show both the group summary and individual nodes
	wants := []string{
		"4 pending",                // Total count
		"3 nodes share SHA256:abc", // Group summary (prefix match)
		"web01", "web02", "web03",  // Nodes in group
		"lb01", // Unique node
	}
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Errorf("onboarding missing %q", want)
		}
	}
}

// TestOnboardingStepNavigation verifies the step counts render correctly.
func TestOnboardingStepNavigation(t *testing.T) {
	s, db := newTestServer(t)
	ctx := t.Context()

	db.CreateUser(ctx, "admin", "a good long password", "admin", "Admin", false)
	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	adminID := int64(1)

	// Add 5 nodes
	for i := 1; i <= 5; i++ {
		addNode(t, db, fmt.Sprintf("192.0.2.%d", i), fmt.Sprintf("node%d", i), adminID)
	}

	// Add 2 pending keys
	for i := 1; i <= 2; i++ {
		seedPendingKey(t, s, fmt.Sprintf("SHA256:fp%d", i),
			fmt.Sprintf("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIfp%d", i), fmt.Sprintf("node%d", i))
	}

	w := c.get("/onboarding")
	if w.Code != 200 {
		t.Fatalf("GET /onboarding = %d, want 200", w.Code)
	}

	body := w.Body.String()

	// Nav should show counts
	wants := []string{
		"1 · Nodes",                        // Step 1
		"<span class=\"ct\">5</span>",      // Node count
		"3 · Host keys",                    // Step 3
		"<span class=\"ct warn\">2</span>", // Pending keys in warn style
	}
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Errorf("step nav missing %q", want)
		}
	}
}
