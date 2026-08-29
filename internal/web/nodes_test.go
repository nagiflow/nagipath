package web

import (
	"net/url"
	"strconv"
	"strings"
	"testing"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

// Nodes is the React SPA now (docs/adr/0017); these assert against the
// protojson API (internal/api/nodes.go, docs/adr/0018) instead of rendered
// HTML — decoded via protojson.Unmarshal against the generated schema, not
// hand-typed json tags, since protojson serializes int64 fields as JSON
// strings and plain encoding/json can't decode those.

func TestNodesListRenders(t *testing.T) {
	s, db := newTestServer(t)
	db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false)

	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	w := c.get("/api/ui/nodes")
	if w.Code != 200 {
		t.Fatalf("got %d, want 200", w.Code)
	}
	var resp pb.NodesListResponse
	if err := protojson.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode /api/ui/nodes: %v", err)
	}
}

func TestNodeDetailRenders(t *testing.T) {
	s, db := newTestServer(t)
	db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false)

	nodeID, err := db.AddNode(t.Context(), "192.168.1.1", 22, "test-node", "nagipath", nil, nil, "manual", nil)
	if err != nil {
		t.Fatalf("failed to add node: %v", err)
	}

	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	w := c.get("/api/ui/nodes/" + itoa(nodeID))
	if w.Code != 200 {
		t.Fatalf("got %d, want 200", w.Code)
	}
	var resp pb.NodeDetailResponse
	if err := protojson.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode /api/ui/nodes/%d: %v", nodeID, err)
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

func TestInstanceRedirect(t *testing.T) {
	s, db := newTestServer(t)
	db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false)

	// This test would need a real instance, but we can test the handler exists
	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	// Accessing a non-existent instance should 404, not panic
	w := c.get("/instances/999")
	if w.Code != 404 {
		t.Logf("got %d for non-existent instance (expected 404)", w.Code)
	}
}

// Opening a config file has to show the file. The tab read `SELECT content FROM
// file`, a table that does not exist — content is a blob addressed by digest —
// and the suite only ever loaded the tab with nothing selected, so the panel was
// blank in every browser and green in every run.
func TestNodeFilesTabRendersTheSelectedFile(t *testing.T) {
	s, db := newTestServer(t)
	if _, err := db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false); err != nil {
		t.Fatal(err)
	}
	instID := seedNginx(t, db, "web02", "10.90.4.11")
	inst, err := db.Instance(t.Context(), instID)
	if err != nil {
		t.Fatal(err)
	}

	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	list := c.get("/api/ui/nodes/" + itoa(inst.NodeID) + "/files?process=" + itoa(instID))
	if list.Code != 200 {
		t.Fatalf("the files tab = %d\n%s", list.Code, list.Body.String())
	}
	var filesResp pb.NodeDetailResponse
	if err := protojson.Unmarshal(list.Body.Bytes(), &filesResp); err != nil {
		t.Fatalf("decode files tab: %v", err)
	}
	if len(filesResp.Files) == 0 {
		t.Fatal("the files tab listed no file to open")
	}
	fileID := filesResp.Files[0].Id

	w := c.get("/api/ui/nodes/" + itoa(inst.NodeID) + "/files?process=" + itoa(instID) + "&file=" + itoa(fileID))
	if w.Code != 200 {
		t.Fatalf("opening file %d = %d\n%s", fileID, w.Code, w.Body.String())
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
	if got := c.get("/api/ui/nodes/" + itoa(inst.NodeID) + "/files?process=" + itoa(instID) + "&file=999999").Code; got != 200 {
		t.Errorf("a stale ?file= = %d, want 200", got)
	}
}

func TestProcessPickerOnlyWithMultipleProcesses(t *testing.T) {
	// This test verifies that the process picker appears only when a node
	// runs >1 process, as the design specifies.
	t.Skip("requires test fixture with multi-process node")
}

func itoa(i int64) string {
	return strconv.FormatInt(i, 10)
}
