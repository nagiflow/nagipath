package api

import (
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
	"github.com/nagiflow/nagipath/internal/store"
	"google.golang.org/protobuf/encoding/protojson"
)

func testDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "web.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// TestGetDashboardMarksQuarantinedNodes is the API-side half of
// internal/web's old TestQuarantineSmoke (see quarantine_test.go): a node
// that has crossed the quarantine threshold must show up in the dashboard's
// attention list with kind "QUARANTINED", not just "DEGRADED".
func TestGetDashboardMarksQuarantinedNodes(t *testing.T) {
	db := testDB(t)
	ctx := t.Context()
	nodeID, err := db.AddNode(ctx, "10.90.4.9", 22, "lb09", "nagipath", nil, nil, "manual", nil)
	if err != nil {
		t.Fatal(err)
	}
	for range 12 { // default threshold is 10
		if err := db.NodeFailure(ctx, nodeID, true); err != nil {
			t.Fatal(err)
		}
	}

	s := New(db, nil, false)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/dashboard", nil)
	s.getDashboard(w, r)

	if w.Code != 200 {
		t.Fatalf("getDashboard status = %d, body %q", w.Code, w.Body.String())
	}
	var resp pb.DashboardResponse
	if err := protojson.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode dashboard response: %v", err)
	}
	found := false
	for _, a := range resp.Attention {
		if a.Kind == "QUARANTINED" {
			found = true
		}
	}
	if !found {
		t.Errorf("dashboard attention list has no QUARANTINED entry: %+v", resp.Attention)
	}
}

// The activity strip is arithmetic on timestamps, and off-by-one bucketing is
// invisible on screen: a bar in the wrong place still looks like a bar.
// Ported from internal/web/dashboard_test.go's TestActivityBuckets — same
// fixture, same assertions, against the JSON handler's copy of the logic.
func TestDashboardActivityBuckets(t *testing.T) {
	now := time.Date(2026, 8, 24, 14, 40, 0, 0, time.UTC)
	at := func(d time.Duration, status string) store.Collection {
		return store.Collection{StartedAt: now.Add(d).Format("2006-01-02T15:04:05Z"), Status: status}
	}

	got, max := dashboardActivity(now, []store.Collection{
		at(-23*time.Hour-10*time.Minute, "succeeded"),
		at(-30*time.Minute, "failed"),
		at(-10*time.Minute, "succeeded"),
		at(-5*time.Minute, "degraded"),
		at(-30*time.Hour, "succeeded"),
		{StartedAt: "not a timestamp"},
	})

	if len(got) != 24 {
		t.Fatalf("buckets = %d, want 24", len(got))
	}
	if got[0].Total != 1 {
		t.Errorf("oldest bucket total = %d, want 1", got[0].Total)
	}
	if got[23].Total != 3 || got[23].Failed != 1 || got[23].Degraded != 1 {
		t.Errorf("newest bucket = %d total / %d failed / %d degraded, want 3/1/1", got[23].Total, got[23].Failed, got[23].Degraded)
	}
	if max != 3 {
		t.Errorf("max = %d, want 3", max)
	}
	var sum int
	for _, b := range got {
		sum += b.Total
	}
	if sum != 4 {
		t.Errorf("counted %d collections, want 4 — the 30h-old row and the bad timestamp must be dropped", sum)
	}
}

func TestAttentionTone(t *testing.T) {
	cases := map[string]string{
		"QUARANTINED": "err",
		"DEGRADED":    "deg",
		"CERT":        "deg",
		"STALE":       "deg",
		"DRIFT":       "deg",
		"HOST KEY":    "inf",
		"LOG FORMAT":  "inf",
	}
	for kind, want := range cases {
		if got := attentionTone(kind); got != want {
			t.Errorf("attentionTone(%q) = %q, want %q", kind, got, want)
		}
	}
}
