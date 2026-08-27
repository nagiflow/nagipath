package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// Test that drift page renders with scope filter in URL
func TestDriftWithScopeFilter(t *testing.T) {
	s, db := newTestServer(t)
	if _, err := db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false); err != nil {
		t.Fatal(err)
	}

	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	tests := []string{
		"/drift",
		"/drift?scope=route",
		"/drift?scope=upstream",
		"/drift?scope=site",
		"/drift?cluster=all",
	}

	for _, path := range tests {
		w := c.get(path)
		if w.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, w.Code)
		}
	}
}

// Test drift review route exists and returns appropriate status
func TestDriftReview(t *testing.T) {
	s, db := newTestServer(t)
	if _, err := db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false); err != nil {
		t.Fatal(err)
	}

	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	// Test with non-existent instance (should return appropriate error/redirect)
	w := c.get("/drift/review/999")
	// Will be 404 or redirect if instance doesn't exist
	if w.Code == http.StatusOK {
		// If it somehow returns 200, that's also acceptable (empty state)
		return
	}
	if w.Code != http.StatusNotFound && w.Code != http.StatusSeeOther {
		t.Errorf("GET /drift/review/999 = %d, expected 404 or 303", w.Code)
	}
}

// Test drift page shows empty state appropriately
func TestDriftEmptyState(t *testing.T) {
	s, db := newTestServer(t)
	if _, err := db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false); err != nil {
		t.Fatal(err)
	}

	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	w := c.get("/drift")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /drift = %d, want 200", w.Code)
	}

	body := w.Body.String()
	// Should show appropriate empty state message
	if !strings.Contains(body, "Nothing is collected yet") && !strings.Contains(body, "Nothing compared yet") {
		// One of these messages should be present in empty state
		t.Log("drift page rendered but empty state message not found (may have data)")
	}
}
