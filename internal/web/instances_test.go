package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// The filter selects on the instances page must actually narrow the list, and
// the CSV export must carry the filtered set, not everything.
// OBSOLETE: /instances was removed in the design alignment. The node-centric
// design does not have a fleet-wide instances list with filtering.
func TestInstancesFilterAndExport(t *testing.T) {
	t.Skip("instances list removed; /instances redirects to /nodes")
	s, db := newTestServer(t)
	if _, err := db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false); err != nil {
		t.Fatal(err)
	}

	// Seed two instances with different vendors.
	_ = seedNginx(t, db, "web01", "10.90.4.2")
	apacheID := seedNginx(t, db, "web02", "10.90.4.3")
	// Change the second instance to Apache so we can filter by vendor.
	if _, err := db.W.ExecContext(t.Context(), `UPDATE instance SET vendor = 'apache' WHERE id = ?`, apacheID); err != nil {
		t.Fatal(err)
	}

	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	// Unfiltered list should show both instances.
	w := c.get("/instances")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /instances = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "web01 nginx") {
		t.Errorf("unfiltered list missing nginx instance")
	}
	if !strings.Contains(body, "web02") {
		t.Errorf("unfiltered list missing apache instance")
	}

	// Filter by vendor=nginx should show only the nginx instance.
	w = c.get("/instances?vendor=nginx")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /instances?vendor=nginx = %d, want 200", w.Code)
	}
	body = w.Body.String()
	if !strings.Contains(body, "web01 nginx") {
		t.Errorf("vendor=nginx filter missing nginx instance")
	}
	if strings.Contains(body, "web02") && strings.Contains(body, "apache") {
		t.Errorf("vendor=nginx filter should not show apache instance")
	}

	// CSV export of the filtered set should contain only nginx.
	w = c.get("/instances?vendor=nginx&export=csv")
	if got := w.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/csv") {
		t.Errorf("export Content-Type = %q, want text/csv", got)
	}
	csv := w.Body.String()
	lines := strings.Split(strings.TrimSpace(csv), "\n")
	if len(lines) != 2 {
		t.Fatalf("filtered export has %d line(s), want header + 1 row:\n%s", len(lines), csv)
	}
	if !strings.HasPrefix(lines[0], "instance,node,vendor,") {
		t.Errorf("export missing CSV header: %q", lines[0])
	}
	if !strings.Contains(lines[1], "nginx") {
		t.Errorf("export row missing nginx: %q", lines[1])
	}
	if strings.Contains(csv, "apache") {
		t.Errorf("filtered export leaked apache instance:\n%s", csv)
	}

	// Export with no filter should contain both.
	w = c.get("/instances?export=csv")
	csv = w.Body.String()
	if !strings.Contains(csv, "nginx") || !strings.Contains(csv, "apache") {
		t.Errorf("unfiltered export missing instances:\n%s", csv)
	}
}
