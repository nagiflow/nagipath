package web

import (
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nagiflow/nagipath/internal/store"
)

// /certificates has to render at all: it is one ParseFS set, and a field that
// only exists on one branch of one template takes every page down with it.
func TestCertificatesRenders(t *testing.T) {
	s, db := newTestServer(t)
	if _, err := db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false); err != nil {
		t.Fatal(err)
	}

	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	w := c.get("/certificates")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /certificates = %d\n%s", w.Code, w.Body.String())
	}
	// No certificate fixture exists, so this is the empty state, and the empty
	// state has to say which of the three empties it is.
	if !strings.Contains(w.Body.String(), "No certificates seen yet") {
		t.Error("/certificates with no certificates does not say so")
	}
}

// The expiry tiles are labelled cumulatively — "≤ 30 days" includes what expires
// tomorrow — so they have to count that way. Exclusive buckets printed "≤ 7 days 1"
// beside "≤ 30 days 1" on a fleet holding two certificates inside thirty days, and
// the tile an operator escalates on undercounted the problem.
func TestCertWindowsAreCumulative(t *testing.T) {
	in := func(d time.Duration, bindings int) store.CertificateView {
		return store.CertificateView{
			Certificate: store.Certificate{NotAfter: time.Now().Add(d).UTC().Format(time.RFC3339)},
			Bindings:    bindings,
		}
	}
	got := certWindows([]store.CertificateView{
		in(-48*time.Hour, 3),  // expired
		in(3*24*time.Hour, 4), // ≤ 7 days, and so also ≤ 30
		in(20*24*time.Hour, 5),
		in(200*24*time.Hour, 6), // outside every window, still distinct
		{Certificate: store.Certificate{NotAfter: "not a timestamp"}, Bindings: 9},
	})

	want := []Stat{
		{N: 1, Label: "expired", Tone: "warn", Note: "3 bindings"},
		{N: 1, Label: "≤ 7 days", Tone: "warn", Note: "4 bindings"},
		{N: 2, Label: "≤ 30 days", Tone: "warn", Note: "9 bindings"},
		{N: 5, Label: "total distinct", Note: "by fingerprint"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("certWindows:\n got %+v\nwant %+v", got, want)
	}
}

// .Hosts is a comma-joined list of node display names, so the template printing
// "{{.Hosts}} hosts" rendered "app01 hosts".
func TestNodeCountCountsRatherThanEchoes(t *testing.T) {
	for in, want := range map[string]string{
		"":                  "unbound",
		"app01":             "1 node",
		"app01,web02":       "2 nodes",
		"app01,web02,web03": "3 nodes",
	} {
		if got := nodeCount(in); got != want {
			t.Errorf("nodeCount(%q) = %q, want %q", in, got, want)
		}
	}
}

// Test that filter query parameters are accepted and render without error
func TestCertificatesFilters(t *testing.T) {
	s, db := newTestServer(t)
	if _, err := db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false); err != nil {
		t.Fatal(err)
	}

	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	tests := []string{
		"/certificates?expires=30d",
		"/certificates?expires=7d",
		"/certificates?expires=expired",
		"/certificates?issuer=test",
		"/certificates?cluster=1",
		"/certificates?include_cas=1",
		"/certificates?expires=7d&cluster=1",
	}

	for _, path := range tests {
		w := c.get(path)
		if w.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, w.Code)
		}
	}
}

// Test certificate detail route returns 404 for non-existent certificates
func TestCertificateDetailNotFound(t *testing.T) {
	s, db := newTestServer(t)
	if _, err := db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false); err != nil {
		t.Fatal(err)
	}

	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	w := c.get("/certificates/999")
	if w.Code != http.StatusNotFound {
		t.Errorf("GET /certificates/999 = %d, want 404", w.Code)
	}
}
