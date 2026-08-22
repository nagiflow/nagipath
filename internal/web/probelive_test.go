package web

import (
	"testing"

	"github.com/nagiflow/nagipath/internal/probe"
)

// The one decision in the live-run registry: verified is final. A hop proved it
// handled the request, so a later step on the same hop — a second log path that did
// not match, or a stale "checking" — must not take the green ring off it.
func TestProbeRunStateVerifiedIsFinal(t *testing.T) {
	r := &probeRun{state: map[int]string{}}
	for _, s := range []probe.Step{
		{Ordinal: -1, Text: "GET https://shop.example.com/v2/charge"},
		{Ordinal: 0, State: probe.StateChecking},
		{Ordinal: 0, State: probe.StateVerified},
		{Ordinal: 0, State: probe.StateMissing},
		{Ordinal: 1, State: probe.StateChecking},
		{Ordinal: 1, State: probe.StateMissing},
	} {
		r.add(s)
	}
	v := r.view()
	if v.State[0] != probe.StateVerified {
		t.Errorf("hop 0 = %q, want %q", v.State[0], probe.StateVerified)
	}
	if v.State[1] != probe.StateMissing {
		t.Errorf("hop 1 = %q, want %q", v.State[1], probe.StateMissing)
	}
	if _, ok := v.State[-1]; ok {
		t.Error("a step that is not about a hop put a state on the graph")
	}
	if len(v.Steps) != 6 {
		t.Errorf("view kept %d steps, want all 6", len(v.Steps))
	}
	// The view is a copy: a render must not see the goroutine's map change mid-page.
	r.add(probe.Step{Ordinal: 1, State: probe.StateVerified})
	if v.State[1] != probe.StateMissing || len(v.Steps) != 6 {
		t.Error("view() aliases the run instead of copying it")
	}
}
