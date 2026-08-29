package web

import (
	"testing"
)

// The two component constructors that make a decision rather than just carrying
// values: a width read off a spec, and a stat dropped because it has nothing to
// report.
func TestComponentConstructors(t *testing.T) {
	got := cols("node", "vendor:6rem", ":1.4rem")
	want := []column{{Label: "node"}, {Label: "vendor", Width: "6rem"}, {Width: "1.4rem"}}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("cols()[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}

	list := stats(
		stat(0, "nodes", "/nodes", ""),
		statif(false, 0, "host keys to approve", "/settings/hostkeys", "warn"),
	)
	if len(list) != 1 || list[0].Label != "nodes" {
		t.Fatalf("stats() = %+v, want the zero-count stat kept and the statif dropped", list)
	}
	if tone := warnif(0); tone != "" {
		t.Errorf("warnif(0) = %q, want no tone", tone)
	}
}

