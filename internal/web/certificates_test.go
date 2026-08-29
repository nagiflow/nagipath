package web

import (
	"net/http"
	"net/url"
	"reflect"
	"testing"
	"time"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
	"github.com/nagiflow/nagipath/internal/store"
	"google.golang.org/protobuf/encoding/protojson"
)

// Certificates is the React SPA now (docs/adr/0017); GET /api/ui/certificates
// has to render at all — same guarantee TestCertificatesRenders always
// checked, just against the JSON API (docs/adr/0018) instead of HTML.
func TestCertificatesRenders(t *testing.T) {
	s, db := newTestServer(t)
	if _, err := db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false); err != nil {
		t.Fatal(err)
	}

	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	w := c.get("/api/ui/certificates")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/ui/certificates = %d\n%s", w.Code, w.Body.String())
	}
	var resp pb.CertificatesListResponse
	if err := protojson.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode /api/ui/certificates: %v", err)
	}
	// No certificate fixture exists, so this is the empty state.
	if len(resp.List) != 0 {
		t.Errorf("List = %+v, want none with no certificates seen yet", resp.List)
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
		"/api/ui/certificates?expires=30d",
		"/api/ui/certificates?expires=7d",
		"/api/ui/certificates?expires=expired",
		"/api/ui/certificates?issuer=test",
		"/api/ui/certificates?cluster=1",
		"/api/ui/certificates?include_cas=1",
		"/api/ui/certificates?expires=7d&cluster=1",
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

	w := c.get("/api/ui/certificates/999")
	if w.Code != http.StatusNotFound {
		t.Errorf("GET /api/ui/certificates/999 = %d, want 404", w.Code)
	}
}
