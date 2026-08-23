package web

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// TestQuarantineSmoke confirms the quarantine badge renders on dashboard,
// nodes list and node detail without a template error when a Node crosses
// the threshold — the one check a rendering change across three pages needs.
func TestQuarantineSmoke(t *testing.T) {
	s, db := newTestServer(t)
	ctx := t.Context()
	db.CreateUser(ctx, "admin", "a good long password", "admin", "Admin", false)
	nodeID, err := db.AddNode(ctx, "10.90.4.9", 22, "lb09", "nagipath", nil, nil, "manual", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Default threshold is 10; fail 12 times in a row to cross it.
	for i := 0; i < 12; i++ {
		if err := db.NodeFailure(ctx, nodeID, true); err != nil {
			t.Fatal(err)
		}
	}

	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})
	for _, path := range []string{
		"/",
		"/nodes",
		"/nodes/" + strconv.FormatInt(nodeID, 10),
	} {
		w := c.get(path)
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s = %d\n%s", path, w.Code, w.Body.String())
		}
		body := w.Body.String()
		if !strings.Contains(body, "</html>") {
			t.Fatalf("GET %s rendered a truncated page (template error mid-render)", path)
		}
		if !strings.Contains(body, "QUAR") {
			t.Errorf("GET %s: expected a QUAR badge, got none\n%s", path, body)
		}
	}
}
