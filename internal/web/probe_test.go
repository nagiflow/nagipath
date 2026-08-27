package web

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/nagiflow/nagipath/internal/store"
	"github.com/nagiflow/nagipath/internal/trace"
)

// TestProbeScreenRequiresAuth verifies the probe screen redirects when signed out.
func TestProbeScreenRequiresAuth(t *testing.T) {
	s, db := newTestServer(t)
	db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false)
	c := &client{t: t, s: s}
	w := c.get("/trace/probe?probe=1")
	if w.Code != http.StatusSeeOther || !strings.HasPrefix(w.Header().Get("Location"), "/login") {
		t.Errorf("GET /trace/probe while signed out = %d -> %q", w.Code, w.Header().Get("Location"))
	}
}

// TestViewerCannotReachProbeScreen verifies viewers are refused outright.
func TestViewerCannotReachProbeScreen(t *testing.T) {
	s, db := newTestServer(t)
	db.CreateUser(t.Context(), "viewer", "a good long password", "viewer", "Viewer", false)
	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"viewer"}, "password": {"a good long password"}})
	if c.cookie == "" {
		t.Fatal("viewer could not sign in")
	}

	if got := c.get("/trace/probe?probe=1").Code; got != http.StatusForbidden {
		t.Errorf("GET /trace/probe as a viewer = %d, want 403", got)
	}
}

// TestProbeScreenRenders verifies the probe screen renders for an admin with
// the expected panels and stored probe data.
func TestProbeScreenRenders(t *testing.T) {
	s, db := newTestServer(t)
	ctx := t.Context()
	db.CreateUser(ctx, "admin", "a good long password", "admin", "Admin", false)

	// Create a minimal trace
	top, err := trace.Load(ctx, s.DB)
	if err != nil {
		t.Fatalf("Load topology: %v", err)
	}
	q := trace.Query{Scheme: "https", Hostname: "example.com", Path: "/"}
	tr := trace.Walk(top, q)
	traceID, _ := trace.Save(ctx, s.DB, tr, nil)

	// Create a probe
	adminID := int64(1) // First user created
	res, _ := s.DB.W.ExecContext(ctx, `INSERT INTO probe
		(trace_id, actor_user_id, method, url, correlation_token, origin_host,
		 requested_at, status_code, duration_ms, result)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		traceID, adminID, "GET", "https://example.com/", "abc123def456",
		"localhost", store.Now(), 200, 100, "completed")
	probeID, _ := res.LastInsertId()

	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	w := c.get("/trace/probe?probe=" + strconv.FormatInt(probeID, 10))
	if w.Code != http.StatusOK {
		t.Fatalf("GET /trace/probe = %d, want 200: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	if !strings.Contains(body, "</html>") {
		t.Fatal("probe screen rendered a truncated page (template error mid-render)")
	}

	wants := []string{
		"Probe",            // Title
		"Request",          // Request panel
		"Audit",            // Audit panel
		"Response",         // Response stat
		"Hops confirmed",   // Hops stat
		"State changes",    // State changes stat
		"abc123",           // Correlation token (shortened)
		"manual · audited", // Badge
	}
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Errorf("probe screen missing %q", want)
		}
	}
}

// TestProbeScreenWithEvidence verifies the hop evidence table and matched log
// lines render when probe evidence exists.
func TestProbeScreenWithEvidence(t *testing.T) {
	s, db := newTestServer(t)
	ctx := t.Context()
	db.CreateUser(ctx, "admin", "a good long password", "admin", "Admin", false)

	// A real instance to hang the evidence on. Evidence with instance_id 1 and no
	// instance in the database is a foreign-key failure, and this fixture discarded
	// the error — the panel under test had nothing to render and never could.
	instID := seedNginx(t, db, "web02", "10.90.4.11")

	// Create trace against a hostname the fixture actually serves, so the walk
	// produces the hop the evidence is evidence about.
	top, err := trace.Load(ctx, s.DB)
	if err != nil {
		t.Fatal(err)
	}
	q := trace.Query{Scheme: "https", Hostname: "shop.example.com", Path: "/", Port: 443}
	tr := trace.Walk(top, q)
	traceID, err := trace.Save(ctx, s.DB, tr, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Create probe with evidence
	adminID := int64(1)
	res, err := s.DB.W.ExecContext(ctx, `INSERT INTO probe
		(trace_id, actor_user_id, method, url, correlation_token, origin_host,
		 requested_at, status_code, duration_ms, result)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		traceID, adminID, "GET", "https://shop.example.com/", "token123",
		"localhost", store.Now(), 200, 50, "completed")
	if err != nil {
		t.Fatal(err)
	}
	probeID, _ := res.LastInsertId()

	// Add evidence row
	if _, err := s.DB.W.ExecContext(ctx, `INSERT INTO probe_evidence
		(probe_id, instance_id, kind, raw_evidence, grants, observed_at, parsed_fields)
		VALUES (?,?,?,?,?,?,?)`,
		probeID, instID, "access_log_line", "192.0.2.1 - [26/Aug/2026:04:33:02 +0000] GET /test 200",
		"verified", store.Now(), `{"prior_confidence":"inferred"}`); err != nil {
		t.Fatal(err)
	}

	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	w := c.get("/trace/probe?probe=" + strconv.FormatInt(probeID, 10))
	if w.Code != http.StatusOK {
		t.Fatalf("GET /trace/probe = %d, want 200", w.Code)
	}

	body := w.Body.String()
	wants := []string{
		"Hop evidence",                      // Table title
		"INFERRED",                          // Before state
		"VERIFIED",                          // After state
		"Matched log lines",                 // Log lines panel
		"192.0.2.1 - [26/Aug/2026:04:33:02", // Log line content
	}
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Errorf("evidence panel missing %q", want)
		}
	}
}

// TestProbeScreenNotFound verifies missing probe redirects back to history.
func TestProbeScreenNotFound(t *testing.T) {
	s, db := newTestServer(t)
	db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false)
	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	w := c.get("/trace/probe?probe=99999")
	if w.Code != http.StatusSeeOther {
		t.Errorf("missing probe: got %d, want 303", w.Code)
	}
	if loc := w.Header().Get("Location"); !strings.HasPrefix(loc, "/trace/history") {
		t.Errorf("redirects to %q, want /trace/history", loc)
	}
}

// TestProbeScreenEmptyForm verifies the empty form renders when no probe is given.
func TestProbeScreenEmptyForm(t *testing.T) {
	s, db := newTestServer(t)
	db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false)
	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	w := c.get("/trace/probe")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /trace/probe = %d, want 200", w.Code)
	}

	body := w.Body.String()
	// Should show empty form with empty state
	wants := []string{
		"Probe",                     // Title
		"Request",                   // Form panel
		"Run probe",                 // Submit button
		"No probe has been run yet", // Empty state
		"action=\"/trace/probe\"",   // Form action
	}
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Errorf("empty probe form missing %q", want)
		}
	}

	// Should NOT show results panels
	unwanted := []string{
		"Audit",        // Only shown with results
		"Hop evidence", // Only shown with results
	}
	for _, unwant := range unwanted {
		if strings.Contains(body, unwant) {
			t.Errorf("empty form should not show %q", unwant)
		}
	}
}
