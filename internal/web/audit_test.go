package web

import (
	"net/url"
	"strings"
	"testing"
)

func TestAuditActorFilter(t *testing.T) {
	s, db := newTestServer(t)
	ctx := t.Context()
	adminID, err := db.CreateUser(ctx, "admin", "a good long password", "admin", "Admin", false)
	if err != nil {
		t.Fatal(err)
	}

	// Record an audit event.
	if err := db.Audit(ctx, &adminID, "test.action", "test_kind", nil, "test_target"); err != nil {
		t.Fatal(err)
	}

	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	// Test the actor filter.
	w := c.get("/settings/audit?actor=admin")
	body := w.Body.String()
	if !strings.Contains(body, "admin") {
		t.Error("Actor filter did not match expected username")
	}
}

func TestAuditActionFilter(t *testing.T) {
	s, db := newTestServer(t)
	ctx := t.Context()
	adminID, err := db.CreateUser(ctx, "admin", "a good long password", "admin", "Admin", false)
	if err != nil {
		t.Fatal(err)
	}

	if err := db.Audit(ctx, &adminID, "credential.create", "credential", nil, "test-cred"); err != nil {
		t.Fatal(err)
	}

	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	w := c.get("/settings/audit?action=credential.create")
	body := w.Body.String()
	if !strings.Contains(body, "credential.create") {
		t.Error("Action filter did not return expected action")
	}
}

func TestAuditDateRangeFilter(t *testing.T) {
	s, db := newTestServer(t)
	ctx := t.Context()
	adminID, err := db.CreateUser(ctx, "admin", "a good long password", "admin", "Admin", false)
	if err != nil {
		t.Fatal(err)
	}

	if err := db.Audit(ctx, &adminID, "test.recent", "test_kind", nil, "recent_event"); err != nil {
		t.Fatal(err)
	}

	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	w := c.get("/settings/audit?range=7d")
	body := w.Body.String()
	// The range select should show the selected value.
	if !strings.Contains(body, "Range: 7 days") {
		t.Error("Date range filter not showing selected range")
	}
}

func TestAuditSourceColumn(t *testing.T) {
	s, db := newTestServer(t)
	ctx := t.Context()
	adminID, err := db.CreateUser(ctx, "admin", "a good long password", "admin", "Admin", false)
	if err != nil {
		t.Fatal(err)
	}

	// Record an event with remote_addr.
	if err := db.AuditDetail(ctx, &adminID, "login.success", "user", &adminID, "admin", map[string]any{}, "success", "10.0.0.42"); err != nil {
		t.Fatal(err)
	}

	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	w := c.get("/settings/audit")
	body := w.Body.String()
	// The Source column header should be present.
	if !strings.Contains(body, "Source") {
		t.Error("Source column header not found")
	}
	// The source IP should render for events that have it.
	if !strings.Contains(body, "10.0.0.42") {
		t.Error("Source IP not displayed in table")
	}
}

func TestAuditTotalCount(t *testing.T) {
	s, db := newTestServer(t)
	ctx := t.Context()
	adminID, err := db.CreateUser(ctx, "admin", "a good long password", "admin", "Admin", false)
	if err != nil {
		t.Fatal(err)
	}

	// Record multiple events.
	for i := 0; i < 5; i++ {
		if err := db.Audit(ctx, &adminID, "test.count", "test_kind", nil, "event"); err != nil {
			t.Fatal(err)
		}
	}

	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	w := c.get("/settings/audit")
	body := w.Body.String()
	// The total count should appear somewhere in the page, not hardcoded.
	if !strings.Contains(body, "total") {
		t.Error("Total event count not displayed")
	}
	// Should not say "last 200 events" anymore.
	if strings.Contains(body, "last 200 events") {
		t.Error("Hardcoded 'last 200 events' still present")
	}
}

func TestAuditPagination(t *testing.T) {
	s, db := newTestServer(t)
	ctx := t.Context()
	adminID, err := db.CreateUser(ctx, "admin", "a good long password", "admin", "Admin", false)
	if err != nil {
		t.Fatal(err)
	}

	// Create enough events to trigger pagination.
	for i := 0; i < 60; i++ {
		if err := db.Audit(ctx, &adminID, "test.page", "test_kind", nil, "paginated"); err != nil {
			t.Fatal(err)
		}
	}

	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	w := c.get("/settings/audit?page=2&per_page=50")
	body := w.Body.String()
	// Pagination controls should appear.
	if !strings.Contains(body, "page 2") {
		t.Error("Pagination page indicator not found")
	}
}

func TestAuditPerPageSelect(t *testing.T) {
	s, db := newTestServer(t)
	ctx := t.Context()
	adminID, err := db.CreateUser(ctx, "admin", "a good long password", "admin", "Admin", false)
	if err != nil {
		t.Fatal(err)
	}

	if err := db.Audit(ctx, &adminID, "test.perpage", "test_kind", nil, "per_page_test"); err != nil {
		t.Fatal(err)
	}

	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	w := c.get("/settings/audit?per_page=100")
	body := w.Body.String()
	// The per_page select should exist with the selected value.
	if !strings.Contains(body, "100 / page") {
		t.Error("Per-page select not showing selected value")
	}
}
