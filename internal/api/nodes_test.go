package api

import (
	"net/http/httptest"
	"strconv"
	"testing"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

// TestGetNodesAndNodeMarkQuarantinedNodes is the API-side half of
// internal/web's old TestQuarantineSmoke: a node that has crossed the
// quarantine threshold must show up as "quarantined" — via ConsecutiveFailures
// crossing Threshold — on both the list and detail endpoints, matching the
// same store.Quarantined predicate the old badge relied on.
func TestGetNodesAndNodeMarkQuarantinedNodes(t *testing.T) {
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
	s.getNodes(w, httptest.NewRequest("GET", "/nodes", nil))
	var list pb.NodesListResponse
	if err := protojson.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode /nodes: %v", err)
	}
	if list.Threshold == 0 || list.Quarantined != 1 {
		t.Errorf("getNodes: threshold=%d quarantined=%d, want threshold>0 quarantined=1", list.Threshold, list.Quarantined)
	}

	w = httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/nodes/{id}", nil)
	r.SetPathValue("id", strconv.FormatInt(nodeID, 10))
	s.getNode(w, r)
	if w.Code != 200 {
		t.Fatalf("getNode status = %d, body %q", w.Code, w.Body.String())
	}
	var detail pb.NodeDetailResponse
	if err := protojson.Unmarshal(w.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode /nodes/{id}: %v", err)
	}
	if detail.Node.ConsecutiveFailures < int32(detail.Threshold) {
		t.Errorf("node.consecutive_failures=%d threshold=%d, want failures >= threshold",
			detail.Node.ConsecutiveFailures, detail.Threshold)
	}
}
