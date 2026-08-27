package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// The Export link on a list screen is a promise that what you narrowed to is
// what you get. It is also the one path on the page that never renders a
// template, so a wrong content type or a dropped filter is invisible on screen
// and lands in somebody's spreadsheet instead.
func TestSnapshotsExportIsTheFilteredSet(t *testing.T) {
	s, db := newTestServer(t)
	if _, err := db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false); err != nil {
		t.Fatal(err)
	}
	seedNginx(t, db, "lb01", "10.90.4.2")

	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	if w := c.get("/snapshots"); w.Code != http.StatusOK ||
		!strings.Contains(w.Body.String(), "Recent captures") {
		t.Fatalf("GET /snapshots = %d\n%s", w.Code, w.Body.String())
	}

	w := c.get("/snapshots?export=csv")
	if got := w.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/csv") {
		t.Errorf("export Content-Type = %q, want text/csv", got)
	}
	lines := strings.Split(strings.TrimSpace(w.Body.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("export of one capture wrote %d line(s), want a header and one row:\n%s",
			len(lines), w.Body.String())
	}
	if !strings.HasPrefix(lines[0], "captured_at,instance,node,cluster,trigger") {
		t.Errorf("export has no header row: %q", lines[0])
	}
	if !strings.Contains(lines[1], "lb01 nginx") {
		t.Errorf("export row does not name the instance: %q", lines[1])
	}
	// The node, because "nginx nginx.conf" is every host's instance name: an export
	// that cannot say which host a capture came from is not usable in a spreadsheet.
	if !strings.Contains(lines[1], ",lb01,") {
		t.Errorf("export row does not name the node: %q", lines[1])
	}

	// The filter has to travel with the download. It used to be read, printed
	// back into the select and never applied, so every export was the whole
	// window regardless of what the operator had narrowed to.
	empty := c.get("/snapshots?export=csv&trigger=scheduled")
	if got := strings.Count(strings.TrimSpace(empty.Body.String()), "\n"); got != 0 {
		t.Errorf("filtering to scheduled runs exported %d row(s); the capture was manual", got+1)
	}
}

// Two hosts running stock nginx have the same instance display name — "nginx" plus
// the config basename — so a list that shows only that name shows two identical
// rows. Every screen that lists instances fleet-wide has to name the node.
func TestFleetListsNameTheNode(t *testing.T) {
	s, db := newTestServer(t)
	if _, err := db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false); err != nil {
		t.Fatal(err)
	}
	seedNginx(t, db, "lb01", "10.90.4.2")
	seedNginx(t, db, "lb02", "10.90.4.3")

	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	// /instances changed to /nodes after design alignment.
	for _, path := range []string{"/nodes", "/snapshots", "/search?q=proxy_pass"} {
		body := c.get(path).Body.String()
		for _, host := range []string{"lb01", "lb02"} {
			if !strings.Contains(body, host) {
				t.Errorf("GET %s never names %s, so its two nginx rows are indistinguishable", path, host)
			}
		}
	}
}
