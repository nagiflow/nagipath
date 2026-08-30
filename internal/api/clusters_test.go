package api

import (
	"net/http"
	"strconv"
	"testing"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

// Cluster detail panel rendering is exercised here because a field error only
// surfaces when its branch executes — an untested branch is a 500 in
// production. The panel is conditional on ?cluster=<id>.
func TestClustersRendersDetailPanel(t *testing.T) {
	db := testDB(t)
	ctx := t.Context()

	if _, err := db.CreateUser(ctx, "admin", "a good long password", "admin", "Admin", false); err != nil {
		t.Fatal(err)
	}

	// Create a cluster directly: we just need to verify that the detail panel
	// renders without error when ?cluster=X is provided, regardless of whether
	// it has members (ReconcileClusters may clear them).
	res, err := db.W.ExecContext(ctx,
		`INSERT INTO cluster (name, description, config_hash, created_at) VALUES (?, ?, ?, ?)`,
		"test-cluster", "test cluster description", "abcd1234", "2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	clusterID, _ := res.LastInsertId()

	s := New(db, nil, false)
	c := &authClient{t: t, s: s}
	c.postJSON("/login", map[string]any{"username": "admin", "password": "a good long password"})

	// Decoded via protojson against the generated schema (docs/adr/0018), not
	// hand-typed json tags — a field rename in the .proto can't silently
	// desync this test from the wire format the way a string tag could.
	w := c.get("/clusters")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /clusters = %d, want 200", w.Code)
	}
	var noSel pb.ClustersResponse
	if err := protojson.Unmarshal(w.Body.Bytes(), &noSel); err != nil {
		t.Fatalf("decode /clusters: %v", err)
	}
	if noSel.Selected != nil {
		t.Errorf("no ?cluster= given but a selection was returned: %+v", noSel.Selected)
	}

	// The selected cluster may have zero members after ReconcileClusters runs
	// (it clears cluster_id for instances without parsed config), but the
	// response must still describe it, not 500.
	w = c.get("/clusters?cluster=" + strconv.FormatInt(clusterID, 10))
	if w.Code != http.StatusOK {
		t.Fatalf("GET /clusters?cluster=%d = %d, want 200, body %q", clusterID, w.Code, w.Body.String())
	}
	var withSel pb.ClustersResponse
	if err := protojson.Unmarshal(w.Body.Bytes(), &withSel); err != nil {
		t.Fatalf("decode /clusters?cluster=%d: %v", clusterID, err)
	}
	if withSel.Selected == nil {
		t.Fatalf("?cluster=%d given but no selection was returned", clusterID)
	}
	if withSel.Selected.Name != "test-cluster" {
		t.Errorf("selected.name = %q, want %q", withSel.Selected.Name, "test-cluster")
	}
	// A zero-member selection must not 500 — this cluster was inserted directly
	// via SQL, and ReconcileClusters may clear its members entirely. protojson
	// has no nil-vs-empty distinction for repeated fields (both decode to a nil
	// Go slice, by proto3 design); it's the frontend's fromJson that normalizes
	// this to [] for .map() safety, not this test's job to assert on.
	if len(withSel.Selected.MemberList) != 0 {
		t.Errorf("selected.member_list = %+v, want none", withSel.Selected.MemberList)
	}
}
