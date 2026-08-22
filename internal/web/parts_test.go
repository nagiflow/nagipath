package web

import (
	"testing"

	"github.com/nagiflow/nagipath/internal/probe"
	"github.com/nagiflow/nagipath/internal/trace"
)

// A rank with alternatives is lettered and a rank with one hop is not: the whole
// point of the label is that 1-a and 1-b are one step the request takes once.
func TestHopLabels(t *testing.T) {
	// lb01 → (web02 | web05), and web02 proxies on to app01.
	hops := []*trace.Hop{
		{ArrivedFrom: -1}, {ArrivedFrom: 0}, {ArrivedFrom: 0}, {ArrivedFrom: 1},
	}
	want := map[int]string{0: "0", 1: "1-a", 2: "1-b", 3: "2"}
	got := hopLabels(hops)
	for i, w := range want {
		if got[i] != w {
			t.Errorf("hop %d labelled %q, want %q", i, got[i], w)
		}
	}
	if s := suffix(26); s != "a1" {
		t.Errorf("suffix(26) = %q, want a1 — past the alphabet it must not collide with a", s)
	}
}

// A probe's rows fold onto the hops of the trace: one line per hop, the host as its
// name, and a hop the probe could not raise still in the list saying so.
func TestProbedHops(t *testing.T) {
	hops := []*trace.Hop{
		{Ordinal: 0, Inst: &trace.Inst{NodeName: "lb01", DisplayName: "haproxy haproxy.cfg"}},
		{Ordinal: 1, Inst: &trace.Inst{NodeName: "web02", DisplayName: "nginx nginx.conf"}},
		{Ordinal: 2, IsExternal: true},
	}
	last := &probe.Past{Evidence: []probe.EvidenceRow{
		// Two rows on one hop is one fact, and the stronger of the two wins.
		{HopOrdinal: 1, Instance: "nginx nginx.conf", Grants: "observed_effect", Prior: "inferred"},
		{HopOrdinal: 1, Instance: "nginx nginx.conf", Grants: "verified",
			LogPath: "/var/log/nginx/access.log", Prior: "inferred"},
		// Right instance, wrong hop: the configuration moved, so this proves nothing here.
		{HopOrdinal: 7, Instance: "haproxy haproxy.cfg", Grants: "verified"},
	}}
	got := probed(hops, last)
	if len(got) != 2 {
		t.Fatalf("probed() returned %d hops, want 2 — external hops have no log to read", len(got))
	}
	if got[0].Label != "lb01" || got[0].After != "inferred" {
		t.Errorf("hop 0 = %+v, want lb01 still inferred", got[0])
	}
	if got[1].After != "verified" || got[1].LogPath == "" {
		t.Errorf("hop 1 = %+v, want verified with its log path", got[1])
	}
	if names := stillInferred(got); len(names) != 1 || names[0] != "lb01" {
		t.Errorf("stillInferred() = %v, want [lb01]", names)
	}
}

// The two component constructors that make a decision rather than just carrying
// values: a width read off a spec, and a card dropped because it has nothing to
// report.
func TestComponentConstructors(t *testing.T) {
	got := cols("node", "vendor:6rem", ":1.4rem")
	want := []column{{Label: "node"}, {Label: "vendor", Width: "6rem"}, {Width: "1.4rem"}}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("cols()[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}

	list := cards(
		card(0, "nodes", "/nodes", ""),
		cardif(false, 0, "host keys to approve", "/onboarding", "warn"),
	)
	if len(list) != 1 || list[0].Label != "nodes" {
		t.Fatalf("cards() = %+v, want the zero-count card kept and the cardif dropped", list)
	}
	if tone := warnif(0); tone != "" {
		t.Errorf("warnif(0) = %q, want no tone", tone)
	}
}
