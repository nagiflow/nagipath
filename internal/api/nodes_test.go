package api

import (
	"net/http"
	"strconv"
	"strings"
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
	ns := &nodeService{s: s}

	list, err := ns.ListNodes(ctx, &pb.ListNodesRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if list.Threshold == 0 || list.Quarantined != 1 {
		t.Errorf("ListNodes: threshold=%d quarantined=%d, want threshold>0 quarantined=1", list.Threshold, list.Quarantined)
	}

	detail, err := ns.GetNode(ctx, &pb.GetNodeRequest{Id: nodeID})
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if detail.Node.ConsecutiveFailures < int32(detail.Threshold) {
		t.Errorf("node.consecutive_failures=%d threshold=%d, want failures >= threshold",
			detail.Node.ConsecutiveFailures, detail.Threshold)
	}
}

func TestNodesListRenders(t *testing.T) {
	db := testDB(t)
	db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false)
	s := New(db, nil, false)
	c := &authClient{t: t, s: s}
	c.postJSON("/login", map[string]any{"username": "admin", "password": "a good long password"})

	w := c.get("/nodes")
	if w.Code != 200 {
		t.Fatalf("got %d, want 200", w.Code)
	}
	var resp pb.NodesListResponse
	if err := protojson.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode /nodes: %v", err)
	}
}

func TestNodeDetailRenders(t *testing.T) {
	db := testDB(t)
	db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false)
	nodeID, err := db.AddNode(t.Context(), "192.168.1.1", 22, "test-node", "nagipath", nil, nil, "manual", nil)
	if err != nil {
		t.Fatalf("failed to add node: %v", err)
	}
	s := New(db, nil, false)
	c := &authClient{t: t, s: s}
	c.postJSON("/login", map[string]any{"username": "admin", "password": "a good long password"})

	w := c.get("/nodes/" + strconv.FormatInt(nodeID, 10))
	if w.Code != 200 {
		t.Fatalf("got %d, want 200", w.Code)
	}
	var resp pb.NodeDetailResponse
	if err := protojson.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode /nodes/%d: %v", nodeID, err)
	}
	if resp.Node.DisplayName != "test-node" {
		t.Errorf("Node.DisplayName = %q, want %q", resp.Node.DisplayName, "test-node")
	}
	foundOverview := false
	for _, tab := range resp.Tabs {
		if tab.Label == "Overview" {
			foundOverview = true
		}
	}
	if !foundOverview {
		t.Errorf("Tabs = %+v, want an Overview tab", resp.Tabs)
	}
}

// GetNode's Node.last_collection/last_status came from store.DB.Node, which
// (unlike the list query) never selected them — so the detail page reported
// "never collected" forever, no matter how many collections actually ran.
func TestNodeDetailReportsItsLastCollection(t *testing.T) {
	db := testDB(t)
	if _, err := db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false); err != nil {
		t.Fatal(err)
	}
	nodeID, err := db.AddNode(t.Context(), "10.90.4.9", 22, "lb09", "nagipath", nil, nil, "manual", nil)
	if err != nil {
		t.Fatal(err)
	}
	collectionID, err := db.StartCollection(t.Context(), nodeID, "manual", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.FinishCollection(t.Context(), collectionID, "succeeded", "", 1, 4096, 250); err != nil {
		t.Fatal(err)
	}

	s := New(db, nil, false)
	ns := &nodeService{s: s}
	detail, err := ns.GetNode(t.Context(), &pb.GetNodeRequest{Id: nodeID})
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if detail.Node.LastCollection == "" {
		t.Error("Node.LastCollection is empty, want the finished collection's timestamp")
	}
	if detail.Node.LastStatus != "succeeded" {
		t.Errorf("Node.LastStatus = %q, want succeeded", detail.Node.LastStatus)
	}
}

// Opening a config file has to show the file. The tab read `SELECT content FROM
// file`, a table that does not exist — content is a blob addressed by digest —
// and the suite only ever loaded the tab with nothing selected, so the panel was
// blank in every browser and green in every run.
func TestNodeFilesTabRendersTheSelectedFile(t *testing.T) {
	db := testDB(t)
	if _, err := db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false); err != nil {
		t.Fatal(err)
	}
	instID := seedNginx(t, db, "web02", "10.90.4.11")
	inst, err := db.Instance(t.Context(), instID)
	if err != nil {
		t.Fatal(err)
	}
	s := New(db, nil, false)
	c := &authClient{t: t, s: s}
	c.postJSON("/login", map[string]any{"username": "admin", "password": "a good long password"})

	nodeID := strconv.FormatInt(inst.NodeID, 10)
	instanceID := strconv.FormatInt(instID, 10)
	list := c.get("/nodes/" + nodeID + "/files?process=" + instanceID)
	if list.Code != http.StatusOK {
		t.Fatalf("the files tab = %d\n%s", list.Code, list.Body.String())
	}
	var filesResp pb.NodeDetailResponse
	if err := protojson.Unmarshal(list.Body.Bytes(), &filesResp); err != nil {
		t.Fatalf("decode files tab: %v", err)
	}
	if len(filesResp.Files) == 0 {
		t.Fatal("the files tab listed no file to open")
	}
	fileID := strconv.FormatInt(filesResp.Files[0].Id, 10)

	w := c.get("/nodes/" + nodeID + "/files?process=" + instanceID + "&file=" + fileID)
	if w.Code != http.StatusOK {
		t.Fatalf("opening file %s = %d\n%s", fileID, w.Code, w.Body.String())
	}
	var fileResp pb.NodeDetailResponse
	if err := protojson.Unmarshal(w.Body.Bytes(), &fileResp); err != nil {
		t.Fatalf("decode file body: %v", err)
	}
	// A line only the collected nginx.conf contains, so an empty viewer fails.
	if !strings.Contains(fileResp.FileBody, "server_name") {
		t.Error("the file viewer returned no file body")
	}

	// A ?file= from a stale link selects nothing; it is not a 500.
	if got := c.get("/nodes/" + nodeID + "/files?process=" + instanceID + "&file=999999").Code; got != http.StatusOK {
		t.Errorf("a stale ?file= = %d, want 200", got)
	}
}
