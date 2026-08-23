package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A freshly-migrated test DB (store.Open runs migrations forward) is ready:
// the database answers and nothing is left unapplied.
func TestReadyzOnAFreshlyMigratedDB(t *testing.T) {
	s, _ := newTestServer(t)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "/readyz", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET /readyz = %d, want 200: %s", w.Code, w.Body.String())
	}
	if got := w.Body.String(); got != "ok" {
		t.Errorf("GET /readyz body = %q, want %q", got, "ok")
	}
}

// With no token configured, /metrics is 404 — a security-conscious default so
// fleet-internal counts are never exposed until an operator turns it on.
func TestMetricsWithNoTokenConfiguredIs404(t *testing.T) {
	s, _ := newTestServer(t)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("GET /metrics with no token configured = %d, want 404", w.Code)
	}
}

func TestMetricsRequiresTheConfiguredBearerToken(t *testing.T) {
	s, _ := newTestServer(t)
	s.MetricsToken = "s3cret"

	// No Authorization header at all.
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("GET /metrics with no Authorization header = %d, want 401", w.Code)
	}

	// Wrong token.
	w = httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/metrics", nil)
	r.Header.Set("Authorization", "Bearer wrong")
	s.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("GET /metrics with the wrong token = %d, want 401", w.Code)
	}

	// Correct token.
	w = httptest.NewRecorder()
	r = httptest.NewRequest("GET", "/metrics", nil)
	r.Header.Set("Authorization", "Bearer s3cret")
	s.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /metrics with the correct token = %d, want 200: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "nagipath_nodes_total") {
		t.Errorf("metrics body missing nagipath_nodes_total:\n%s", body)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/plain; version=0.0.4" {
		t.Errorf("Content-Type = %q, want text/plain; version=0.0.4", ct)
	}
}
