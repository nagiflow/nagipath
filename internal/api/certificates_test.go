package api

import (
	"net/http"
	"testing"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

// Certificates is the React SPA (docs/adr/0017); GET /api/certificates has
// to render at all — same guarantee the old internal/web template test
// checked, just against the JSON API (docs/adr/0018) instead of HTML.
func TestCertificatesRenders(t *testing.T) {
	db := testDB(t)
	if _, err := db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false); err != nil {
		t.Fatal(err)
	}
	s := New(db, nil, false)
	c := &authClient{t: t, s: s}
	c.postJSON("/login", map[string]any{"username": "admin", "password": "a good long password"})

	w := c.get("/certificates")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /certificates = %d\n%s", w.Code, w.Body.String())
	}
	var resp pb.CertificatesListResponse
	if err := protojson.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode /certificates: %v", err)
	}
	// No certificate fixture exists, so this is the empty state.
	if len(resp.List) != 0 {
		t.Errorf("List = %+v, want none with no certificates seen yet", resp.List)
	}
}

// Filter query parameters are accepted and render without error.
func TestCertificatesFilters(t *testing.T) {
	db := testDB(t)
	if _, err := db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false); err != nil {
		t.Fatal(err)
	}
	s := New(db, nil, false)
	c := &authClient{t: t, s: s}
	c.postJSON("/login", map[string]any{"username": "admin", "password": "a good long password"})

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

// The certificate detail route returns 404 for a certificate that doesn't exist.
func TestCertificateDetailNotFound(t *testing.T) {
	db := testDB(t)
	if _, err := db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false); err != nil {
		t.Fatal(err)
	}
	s := New(db, nil, false)
	c := &authClient{t: t, s: s}
	c.postJSON("/login", map[string]any{"username": "admin", "password": "a good long password"})

	w := c.get("/certificates/999")
	if w.Code != http.StatusNotFound {
		t.Errorf("GET /certificates/999 = %d, want 404", w.Code)
	}
}
