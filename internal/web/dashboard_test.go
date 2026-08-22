package web

import (
	"testing"
	"time"

	"github.com/nagiflow/nagipath/internal/store"
)

// The activity strip is arithmetic on timestamps, and off-by-one bucketing is
// invisible on screen: a bar in the wrong place still looks like a bar.
func TestActivityBuckets(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Hour)
	at := func(d time.Duration, status string) store.Collection {
		return store.Collection{StartedAt: now.Add(d).Format("2006-01-02T15:04:05Z"), Status: status}
	}

	got, max := activity([]store.Collection{
		at(-23*time.Hour, "succeeded"),   // oldest of the eight three-hour buckets
		at(-30*time.Minute, "failed"),    // newest bucket
		at(-10*time.Minute, "succeeded"), // newest bucket
		at(-40*time.Hour, "succeeded"),   // outside the window
		{StartedAt: "not a timestamp"},   // unparseable, must not panic or count
	})

	if len(got) != 8 {
		t.Fatalf("buckets = %d, want 8", len(got))
	}
	if got[0].Total != 1 {
		t.Errorf("oldest bucket total = %d, want 1", got[0].Total)
	}
	if got[7].Total != 2 || got[7].Failed != 1 {
		t.Errorf("newest bucket = %d total / %d failed, want 2/1", got[7].Total, got[7].Failed)
	}
	if max != 2 {
		t.Errorf("max = %d, want 2", max)
	}
	var sum int
	for _, b := range got {
		sum += b.Total
	}
	if sum != 3 {
		t.Errorf("counted %d collections, want 3 — the 40h-old row and the bad timestamp must be dropped", sum)
	}
}

// Nothing ticked means everything, which is the whole reason the facet column is
// safe to render without a "select all".
func TestMatchesFacet(t *testing.T) {
	if !matchesFacet(nil, "nginx") {
		t.Error("an empty selection must match everything")
	}
	if !matchesFacet([]string{"nginx", "apache"}, "apache") {
		t.Error("a ticked value must match")
	}
	if matchesFacet([]string{"nginx"}, "haproxy") {
		t.Error("an unticked value must not match")
	}
}

// Counts drive the order, so the busiest narrowing is first and ties are stable.
func TestFacetListOrder(t *testing.T) {
	got := facetList(map[string]int{"apache": 2, "nginx": 7, "haproxy": 2})
	want := []string{"nginx", "apache", "haproxy"}
	for i, w := range want {
		if got[i].Value != w {
			t.Fatalf("facet order = %v, want %v", got, want)
		}
	}
}
