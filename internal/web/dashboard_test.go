package web

import (
	"testing"
	"time"

	"github.com/nagiflow/nagipath/internal/store"
)

// The activity strip is arithmetic on timestamps, and off-by-one bucketing is
// invisible on screen: a bar in the wrong place still looks like a bar.
func TestActivityBuckets(t *testing.T) {
	// Mid-hour on purpose: the buckets are hour-aligned, so a run "30 minutes ago"
	// only shares a bucket with now when now is past the half hour. Pinning the
	// clock is what makes that assertion mean something instead of flaking.
	now := time.Date(2026, 8, 24, 14, 40, 0, 0, time.UTC)
	at := func(d time.Duration, status string) store.Collection {
		return store.Collection{StartedAt: now.Add(d).Format("2006-01-02T15:04:05Z"), Status: status}
	}

	got, max := activity(now, []store.Collection{
		// 15:30 yesterday: the window is [15:00 yesterday, 15:00 today), so a flat
		// -23h from 14:40 would fall just outside it rather than in the first bucket.
		at(-23*time.Hour-10*time.Minute, "succeeded"),
		at(-30*time.Minute, "failed"),    // newest bucket
		at(-10*time.Minute, "succeeded"), // newest bucket
		at(-5*time.Minute, "degraded"),   // newest bucket, degraded
		at(-30*time.Hour, "succeeded"),   // outside the window
		{StartedAt: "not a timestamp"},   // unparseable, must not panic or count
	})

	if len(got) != 24 {
		t.Fatalf("buckets = %d, want 24", len(got))
	}
	if got[0].Total != 1 {
		t.Errorf("oldest bucket total = %d, want 1", got[0].Total)
	}
	if got[23].Total != 3 || got[23].Failed != 1 || got[23].Degraded != 1 {
		t.Errorf("newest bucket = %d total / %d failed / %d degraded, want 3/1/1", got[23].Total, got[23].Failed, got[23].Degraded)
	}
	if max != 3 {
		t.Errorf("max = %d, want 3", max)
	}
	var sum int
	for _, b := range got {
		sum += b.Total
	}
	if sum != 4 {
		t.Errorf("counted %d collections, want 4 — the 30h-old row and the bad timestamp must be dropped", sum)
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
