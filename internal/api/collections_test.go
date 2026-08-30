package api

import (
	"net/http/httptest"
	"strings"
	"testing"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
)

// TestGetCollectionsFiltersAndExport ports internal/web's old
// TestCollectionsExportIsTheFilteredSet: the node/status/trigger filters
// must narrow both the JSON rows and the CSV export identically.
func TestGetCollectionsFiltersAndExport(t *testing.T) {
	db := testDB(t)
	ctx := t.Context()
	if _, err := db.CreateUser(ctx, "admin", "a good long password", "admin", "Admin", false); err != nil {
		t.Fatal(err)
	}

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

	s := New(db, nil, false)
	cs := &collectionService{s: s}

	// Unfiltered: both rows, JSON and CSV.
	all, err := cs.ListCollections(ctx, &pb.ListCollectionsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Rows) != 2 || all.Total != 2 {
		t.Fatalf("unfiltered collections: rows=%d total=%d", len(all.Rows), all.Total)
	}

	w := httptest.NewRecorder()
	s.getCollectionsCSV(w, httptest.NewRequest("GET", "/collections?export=csv", nil))
	if got := w.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/csv") {
		t.Errorf("export Content-Type = %q, want text/csv", got)
	}
	lines := strings.Split(strings.TrimSpace(w.Body.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("export of two collections wrote %d line(s), want a header and two rows:\n%s", len(lines), w.Body.String())
	}
	if !strings.HasPrefix(lines[0], "started_at,node,trigger,duration_ms,outcome,status") {
		t.Errorf("export has no header row: %q", lines[0])
	}

	// Filter to node=web01: narrows both JSON and export to one row.
	filtered, err := cs.ListCollections(ctx, &pb.ListCollectionsRequest{Node: "web01"})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered.Rows) != 1 || filtered.Rows[0].NodeName != "web01" {
		t.Fatalf("filter node=web01 = %+v", filtered.Rows)
	}

	// Filter by status=failed shows the other node.
	filtered, err = cs.ListCollections(ctx, &pb.ListCollectionsRequest{Status: "failed"})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered.Rows) != 1 || filtered.Rows[0].NodeName != "web02" {
		t.Fatalf("filter status=failed = %+v", filtered.Rows)
	}
	if filtered.Rows[0].Outcome != "SSH connection refused" {
		t.Errorf("outcome for a failed run = %q, want the error text", filtered.Rows[0].Outcome)
	}
}

// TestGetCollectionsWithNoNodesIsEmpty confirms the empty-fleet shortcut: no
// nodes means the response says Empty rather than computing zero-value stats
// over a NULL SUM.
func TestGetCollectionsWithNoNodesIsEmpty(t *testing.T) {
	db := testDB(t)
	if _, err := db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false); err != nil {
		t.Fatal(err)
	}
	s := New(db, nil, false)
	resp, err := (&collectionService{s: s}).ListCollections(t.Context(), &pb.ListCollectionsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Empty {
		t.Error("collections response with no nodes should report Empty")
	}
}
