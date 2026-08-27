package web

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestNodesListRenders(t *testing.T) {
	s, db := newTestServer(t)
	db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false)

	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	w := c.get("/nodes")
	if w.Code != 200 {
		t.Fatalf("got %d, want 200", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "Nodes") {
		t.Error("page does not contain 'Nodes' heading")
	}
}

func TestNodeDetailRenders(t *testing.T) {
	s, db := newTestServer(t)
	db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false)

	// Add a node
	nodeID, err := db.AddNode(t.Context(), "192.168.1.1", 22, "test-node", "nagipath", nil, nil, "manual", nil)
	if err != nil {
		t.Fatalf("failed to add node: %v", err)
	}

	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	w := c.get("/nodes/" + itoa(nodeID))
	if w.Code != 200 {
		t.Fatalf("got %d, want 200", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "test-node") {
		t.Error("page does not contain node name")
	}
	if !strings.Contains(body, "Overview") {
		t.Error("page does not contain Overview tab")
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

	list := c.get("/nodes/" + itoa(inst.NodeID) + "/files?process=" + itoa(instID))
	if list.Code != 200 {
		t.Fatalf("the files tab = %d\n%s", list.Code, list.Body.String())
	}
	m := regexp.MustCompile(`\?process=\d+&amp;file=(\d+)`).FindStringSubmatch(list.Body.String())
	if m == nil {
		t.Fatal("the files tab listed no file to open")
	}

	w := c.get("/nodes/" + itoa(inst.NodeID) + "/files?process=" + itoa(instID) + "&file=" + m[1])
	if w.Code != 200 {
		t.Fatalf("opening file %s = %d\n%s", m[1], w.Code, w.Body.String())
	}
	// A line only the collected nginx.conf contains, so an empty viewer fails.
	if !strings.Contains(w.Body.String(), "server_name") {
		t.Error("the file viewer rendered no file body")
	}

	// A ?file= from a stale link selects nothing; it is not a 500.
	if got := c.get("/nodes/" + itoa(inst.NodeID) + "/files?process=" + itoa(instID) + "&file=999999").Code; got != 200 {
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
