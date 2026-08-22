package web

import (
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
