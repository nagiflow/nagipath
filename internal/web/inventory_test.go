package web

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// nagipath never scans. Bulk import is the one place a range could get in at
// volume, so every shape that would expand into hosts nobody named is refused
// with a reason — a silent drop in a 200-host import is how a node goes
// uncollected for a month.
func TestInventoryRefusesAnythingThatExpands(t *testing.T) {
	hosts, refused := parseInventory(`
# a wiki paste
lb01.example.com
ops@lb02.example.com:2222
10.90.4.0/24
web[01:20].example.com
10.90.4.10-10.90.4.40
*.example.com
lb01.example.com
ops@
`)
	if len(hosts) != 2 {
		t.Fatalf("hosts = %d (%+v), want lb01 and lb02 only", len(hosts), hosts)
	}
	if hosts[0].Address != "lb01.example.com" || hosts[0].Port != 22 {
		t.Errorf("first host = %+v, want lb01.example.com:22", hosts[0])
	}
	if hosts[1].Address != "lb02.example.com" || hosts[1].Port != 2222 || hosts[1].User != "ops" {
		t.Errorf("second host = %+v, want ops@lb02.example.com:2222", hosts[1])
	}
	// Five refusals: the CIDR, the pattern, the dashed range, the wildcard and the
	// line with no host. The duplicate is deduped silently, which is not a refusal.
	if len(refused) != 5 {
		t.Errorf("refused = %d (%v), want 5", len(refused), refused)
	}
	joined := strings.Join(refused, "\n")
	for _, want := range []string{"10.90.4.0/24", "web[01:20]", "10.90.4.10-10.90.4.40", "*.example.com"} {
		if !strings.Contains(joined, want) {
			t.Errorf("%q was not named in the refusals:\n%s", want, joined)
		}
	}
	if !strings.Contains(joined, "does not scan networks") {
		t.Error("the CIDR refusal must say why, not just that it was skipped")
	}
}

func TestInventoryReadsAnsibleINI(t *testing.T) {
	hosts, _ := parseInventory(`
[edge]
lb01 ansible_host=10.90.4.2 ansible_user=nagipath
lb02 ansible_host=10.90.4.3 ansible_port=2222

[edge:vars]
ansible_user=ignored

[edge:children]
also_ignored
`)
	if len(hosts) != 2 {
		t.Fatalf("hosts = %d (%+v), want 2 — :vars and :children are groups, not hosts", len(hosts), hosts)
	}
	if hosts[0].Name != "lb01" || hosts[0].Address != "10.90.4.2" || hosts[0].User != "nagipath" {
		t.Errorf("lb01 = %+v", hosts[0])
	}
	if hosts[1].Port != 2222 {
		t.Errorf("lb02 port = %d, want 2222", hosts[1].Port)
	}
	if len(hosts[0].Groups) != 1 || hosts[0].Groups[0] != "edge" {
		t.Errorf("groups = %v, want [edge]", hosts[0].Groups)
	}
}

func TestInventoryReadsAnsibleYAML(t *testing.T) {
	hosts, refused := parseInventory(`
all:
  children:
    edge:
      hosts:
        lb01:
          ansible_host: 10.90.4.2
          ansible_port: 2222
        lb02:
          ansible_host: 10.90.4.3
      vars:
        ansible_user: nagipath
`)
	if len(hosts) != 2 {
		t.Fatalf("hosts = %d (%+v), want lb01 and lb02", len(hosts), hosts)
	}
	if hosts[0].Name != "lb01" || hosts[0].Address != "10.90.4.2" || hosts[0].Port != 2222 {
		t.Errorf("lb01 = %+v", hosts[0])
	}
	if hosts[1].Address != "10.90.4.3" || hosts[1].Port != 22 {
		t.Errorf("lb02 = %+v, want the default port", hosts[1])
	}
	if len(refused) != 0 {
		t.Errorf("refused = %v, want none", refused)
	}
}

// The list form is reported as unreadable rather than guessed at: nagipath has no
// YAML dependency and inventing one host from a shape it cannot read is worse
// than saying so.
func TestInventoryRefusesYAMLListForm(t *testing.T) {
	hosts, refused := parseInventory(`
all:
  hosts:
    - lb01
    - lb02
`)
	if len(hosts) != 0 {
		t.Errorf("hosts = %+v, want none", hosts)
	}
	if len(refused) != 2 {
		t.Fatalf("refused = %v, want one line per unreadable entry", refused)
	}
	if !strings.Contains(refused[0], "list form") {
		t.Errorf("refusal = %q, want it to name the shape", refused[0])
	}
}

// Cluster detail panel rendering is exercised here because template field errors
// only surface when their branch executes — an untested branch is a 500 in
// production. The panel is conditional on ?cluster=<id>.
func TestClustersRendersDetailPanel(t *testing.T) {
	s, db := newTestServer(t)
	ctx := t.Context()

	// Create an admin user so we can log in.
	if _, err := db.CreateUser(ctx, "admin", "a good long password", "admin", "Admin", false); err != nil {
		t.Fatal(err)
	}

	// Create a cluster. For this template test, we just need to verify that
	// the detail panel renders without error when ?cluster=X is provided,
	// regardless of whether it has members (ReconcileClusters may clear them).
	res, err := db.W.ExecContext(ctx,
		`INSERT INTO cluster (name, description, config_hash, created_at) VALUES (?, ?, ?, ?)`,
		"test-cluster", "test cluster description", "abcd1234", "2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	clusterID, _ := res.LastInsertId()

	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	// Render without selection: should show "Pick a cluster" panel.
	w := c.get("/clusters")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /clusters = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Pick a cluster") {
		t.Errorf("no-selection state did not render: missing 'Pick a cluster'")
	}

	// Render with selection: should show cluster detail panel. The cluster may
	// have zero members after ReconcileClusters runs (it clears cluster_id for
	// instances without parsed config), but the panel should still render.
	w = c.get("/clusters?cluster=" + strconv.FormatInt(clusterID, 10))
	if w.Code != http.StatusOK {
		t.Fatalf("GET /clusters?cluster=%d = %d, want 200", clusterID, w.Code)
	}
	body = w.Body.String()
	if !strings.Contains(body, "test-cluster") {
		t.Errorf("cluster detail did not render: missing cluster name 'test-cluster'")
	}
	if !strings.Contains(body, "Members") {
		t.Errorf("cluster detail did not render: missing 'Members' section")
	}
	// The template rendering is what we're testing here — content varies
	// based on ReconcileClusters behavior, but both the empty and populated
	// states must render without a 500 (which would happen if a template
	// field reference was wrong).
	if !strings.Contains(body, "</html>") {
		t.Errorf("page did not complete rendering (template error mid-render)")
	}
}
