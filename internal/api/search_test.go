package api

import "testing"

// TestMatchesFacet ports internal/web's old search_test.go: nothing ticked
// means everything, which is the whole reason the facet column is safe to
// render without a "select all".
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

// TestFacetListOrder ports internal/web's old search_test.go: counts drive
// the order, so the busiest narrowing is first and ties are stable.
func TestFacetListOrder(t *testing.T) {
	got := facetList(map[string]int{"apache": 2, "nginx": 7, "haproxy": 2})
	want := []string{"nginx", "apache", "haproxy"}
	for i, w := range want {
		if got[i].Value != w {
			t.Fatalf("facet order = %v, want %v", got, want)
		}
	}
}
