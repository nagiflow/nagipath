package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// The export link on the collections screen promises the filtered set. The
// filters must narrow the table and must survive into the export.
func TestCollectionsExportIsTheFilteredSet(t *testing.T) {
	s, db := newTestServer(t)
	ctx := t.Context()
	if _, err := db.CreateUser(ctx, "admin", "a good long password", "admin", "Admin", false); err != nil {
		t.Fatal(err)
	}

	// Seed two collections: one succeeded, one failed, from different nodes.
	node1, err := db.AddNode(ctx, "10.90.4.2", 22, "web01", "nagipath", nil, nil, "manual", nil)
	if err != nil {
		t.Fatal(err)
	}
	col1, err := db.StartCollection(ctx, node1, "manual", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.FinishCollection(ctx, col1, "succeeded", "", 1, 0, 100); err != nil {
		t.Fatal(err)
	}

	node2, err := db.AddNode(ctx, "10.90.4.3", 22, "web02", "nagipath", nil, nil, "manual", nil)
	if err != nil {
		t.Fatal(err)
	}
	col2, err := db.StartCollection(ctx, node2, "scheduled", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.FinishCollection(ctx, col2, "failed", "SSH connection refused", 0, 0, 50); err != nil {
		t.Fatal(err)
	}

	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	// Unfiltered page renders both collections.
	if w := c.get("/collections"); w.Code != http.StatusOK ||
		!strings.Contains(w.Body.String(), "Collection runs") {
		t.Fatalf("GET /collections = %d\n%s", w.Code, w.Body.String())
	}

	// Unfiltered export includes both.
	w := c.get("/collections?export=csv")
	if got := w.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/csv") {
		t.Errorf("export Content-Type = %q, want text/csv", got)
	}
	lines := strings.Split(strings.TrimSpace(w.Body.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("export of two collections wrote %d line(s), want a header and two rows:\n%s",
			len(lines), w.Body.String())
	}
	if !strings.HasPrefix(lines[0], "started_at,node,trigger,duration_ms,outcome,status") {
		t.Errorf("export has no header row: %q", lines[0])
	}

	// Filter to node=web01 must narrow the rendered table.
	filtered := c.get("/collections?node=web01")
	body := filtered.Body.String()
	if !strings.Contains(body, "web01") {
		t.Errorf("filter node=web01 did not show web01")
	}
	if strings.Contains(body, "web02") {
		t.Errorf("filter node=web01 still shows web02")
	}

	// Filter to node=web01 must narrow the export.
	exportFiltered := c.get("/collections?export=csv&node=web01")
	if got := strings.Count(strings.TrimSpace(exportFiltered.Body.String()), "\n"); got != 1 {
		t.Errorf("filtering to node=web01 exported %d row(s); expected one", got)
	}

	// Filter by status=failed.
	statusFilter := c.get("/collections?status=failed")
	statusBody := statusFilter.Body.String()
	if !strings.Contains(statusBody, "web02") {
		t.Errorf("filter status=failed did not show web02")
	}
	if strings.Contains(statusBody, "web01") {
		t.Errorf("filter status=failed still shows web01")
	}

	// Filter by trigger=manual.
	triggerFilter := c.get("/collections?trigger=manual")
	triggerBody := triggerFilter.Body.String()
	if !strings.Contains(triggerBody, "web01") {
		t.Errorf("filter trigger=manual did not show web01")
	}
	if strings.Contains(triggerBody, "web02") {
		t.Errorf("filter trigger=manual still shows web02 (scheduled)")
	}
}

// Stats tiles must reflect the filtered set, not the whole fleet.
func TestCollectionsStatsAreOverTheFilteredSet(t *testing.T) {
	s, db := newTestServer(t)
	ctx := t.Context()
	if _, err := db.CreateUser(ctx, "admin", "a good long password", "admin", "Admin", false); err != nil {
		t.Fatal(err)
	}

	// Seed three collections with different statuses.
	node1, err := db.AddNode(ctx, "10.90.4.2", 22, "web01", "nagipath", nil, nil, "manual", nil)
	if err != nil {
		t.Fatal(err)
	}
	col1, err := db.StartCollection(ctx, node1, "manual", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.FinishCollection(ctx, col1, "succeeded", "", 1, 0, 100); err != nil {
		t.Fatal(err)
	}

	col2, err := db.StartCollection(ctx, node1, "manual", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.FinishCollection(ctx, col2, "degraded", "", 1, 0, 100); err != nil {
		t.Fatal(err)
	}

	col3, err := db.StartCollection(ctx, node1, "manual", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.FinishCollection(ctx, col3, "failed", "timeout", 0, 0, 50); err != nil {
		t.Fatal(err)
	}

	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	// Unfiltered page shows all three in the stats.
	w := c.get("/collections")
	body := w.Body.String()
	if !strings.Contains(body, ">3<") { // Total runs
		t.Errorf("unfiltered stats do not show 3 runs")
	}
	if !strings.Contains(body, ">1<") { // Succeeded
		t.Errorf("unfiltered stats do not show 1 succeeded")
	}

	// Filter to status=succeeded must show 1 run in stats, not 3.
	filtered := c.get("/collections?status=succeeded")
	filteredBody := filtered.Body.String()
	// The stat tile shows Total=1 when filtered to succeeded.
	lines := strings.Split(filteredBody, "\n")
	var foundTotal bool
	for _, line := range lines {
		if strings.Contains(line, "stat") && strings.Contains(line, ">1<") &&
			strings.Contains(line, "Runs") {
			foundTotal = true
			break
		}
	}
	if !foundTotal {
		t.Errorf("filter status=succeeded does not show Total=1 in stats")
	}
}

func TestCollectionsQueueFigure(t *testing.T) {
	s, db := newTestServer(t)
	ctx := t.Context()
	if _, err := db.CreateUser(ctx, "admin", "a good long password", "admin", "Admin", false); err != nil {
		t.Fatal(err)
	}

	// Create a running collection to ensure the queue figure is non-zero.
	node, err := db.AddNode(ctx, "10.90.4.4", 22, "queue-node", "nagipath", nil, nil, "manual", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.StartCollection(ctx, node, "manual", nil); err != nil {
		t.Fatal(err)
	}

	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	w := c.get("/collections")
	body := w.Body.String()
	// The queue count should appear in the title caption.
	if !strings.Contains(body, "queue") {
		t.Error("Queue figure not displayed in title caption")
	}
}

// A newly added node has no collection rows yet. SQLite returns NULL for a
// bare SUM over that window; the page must still render its zero-value stats.
func TestCollectionsWithNodesButNoRunsRenders(t *testing.T) {
	s, db := newTestServer(t)
	ctx := t.Context()
	if _, err := db.CreateUser(ctx, "admin", "a good long password", "admin", "Admin", false); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AddNode(ctx, "10.90.4.40", 22, "new-node", "nagipath", nil, nil, "manual", nil); err != nil {
		t.Fatal(err)
	}
	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})
	if w := c.get("/collections"); w.Code != http.StatusOK {
		t.Fatalf("GET /collections with no runs = %d\n%s", w.Code, w.Body.String())
	}
}

func TestCollectionsMedianDuration(t *testing.T) {
	s, db := newTestServer(t)
	ctx := t.Context()
	if _, err := db.CreateUser(ctx, "admin", "a good long password", "admin", "Admin", false); err != nil {
		t.Fatal(err)
	}

	// Create several collections with durations.
	node, err := db.AddNode(ctx, "10.90.4.5", 22, "median-node", "nagipath", nil, nil, "manual", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, dur := range []int64{1000, 2000, 3000, 4000, 5000} {
		col, err := db.StartCollection(ctx, node, "scheduled", nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.FinishCollection(ctx, col, "succeeded", "", 0, 0, dur); err != nil {
			t.Fatal(err)
		}
	}

	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	w := c.get("/collections")
	body := w.Body.String()
	// The Median duration stat should exist.
	if !strings.Contains(body, "Median duration") {
		t.Error("Median duration stat not found")
	}
	// The value should be 3000 ms (the median of the above).
	if !strings.Contains(body, "3000") && !strings.Contains(body, "3,000") {
		t.Error("Median duration value not displayed correctly")
	}
}

func TestCollectionsOutcomeAndStateColumns(t *testing.T) {
	s, db := newTestServer(t)
	ctx := t.Context()
	if _, err := db.CreateUser(ctx, "admin", "a good long password", "admin", "Admin", false); err != nil {
		t.Fatal(err)
	}

	node, err := db.AddNode(ctx, "10.90.4.6", 22, "split-node", "nagipath", nil, nil, "manual", nil)
	if err != nil {
		t.Fatal(err)
	}
	col, err := db.StartCollection(ctx, node, "scheduled", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.FinishCollection(ctx, col, "succeeded", "", 2, 0, 100); err != nil {
		t.Fatal(err)
	}

	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	w := c.get("/collections")
	body := w.Body.String()
	// The Outcome column header should be present (separate from State).
	if !strings.Contains(body, "outcome") {
		t.Error("Outcome column header not found")
	}
	// The State column header should be present.
	if !strings.Contains(body, "state") {
		t.Error("State column header not found")
	}
	// The prose outcome (e.g., "2 changes") should render separately from the badge.
	if !strings.Contains(body, "changes") {
		t.Error("Outcome prose not displayed")
	}
	// The badge should appear in the State column.
	if !strings.Contains(body, "SUCCEEDED") && !strings.Contains(body, "succeeded") {
		t.Error("State badge not displayed")
	}
}
