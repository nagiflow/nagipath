package web

import (
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/nagiflow/nagipath/internal/store"
	"github.com/nagiflow/nagipath/internal/trace"
)

// Test that identical-node collapsing collapses.
func TestRuleGroupingCollapses(t *testing.T) {
	// Create mock results with identical rule sets
	res1 := trace.LookupResult{
		Inst: &trace.Inst{NodeName: "node1", Vendor: "nginx"},
		Rules: []trace.LookupRule{
			{Ordinal: 1, Rule: &trace.Rule{Directive: "proxy_pass", Args: "http://backend"}},
		},
	}
	res2 := trace.LookupResult{
		Inst: &trace.Inst{NodeName: "node2", Vendor: "nginx"},
		Rules: []trace.LookupRule{
			{Ordinal: 1, Rule: &trace.Rule{Directive: "proxy_pass", Args: "http://backend"}},
		},
	}
	res3 := trace.LookupResult{
		Inst: &trace.Inst{NodeName: "node3", Vendor: "nginx"},
		Rules: []trace.LookupRule{
			{Ordinal: 1, Rule: &trace.Rule{Directive: "rewrite", Args: "^/api /v1"}},
		},
	}

	results := []trace.LookupResult{res1, res2, res3}
	groups := groupByNode(results)

	// Should have 2 groups: one for the identical pair, one for the different one
	if len(groups) != 2 {
		t.Errorf("expected 2 groups, got %d", len(groups))
	}

	// Find the group with count > 1
	var collapsedGroup *ruleGroup
	for i := range groups {
		if groups[i].Count > 1 {
			collapsedGroup = &groups[i]
			break
		}
	}

	if collapsedGroup == nil {
		t.Fatal("expected to find a collapsed group with count > 1")
	}

	if collapsedGroup.Count != 2 {
		t.Errorf("expected collapsed group to have count 2, got %d", collapsedGroup.Count)
	}

	if len(collapsedGroup.Results) != 2 {
		t.Errorf("expected collapsed group to contain 2 results, got %d", len(collapsedGroup.Results))
	}
}

// Test that rule summary returns the directive verbatim for unknown directives.
func TestRuleSummaryUnknown(t *testing.T) {
	tests := []struct {
		vendor    string
		directive string
		want      string
	}{
		{"nginx", "proxy_pass", "Send to upstream pool or URL"},
		{"nginx", "unknown_directive", "unknown_directive"},
		{"haproxy", "use_backend", "Route to backend based on condition"},
		{"unknown_vendor", "some_directive", "some_directive"},
		{"apache", "ProxyPass", "Reverse proxy to backend URL"},
	}

	for _, tt := range tests {
		got := store.RuleSummaryText(tt.vendor, tt.directive)
		if got != tt.want {
			t.Errorf("RuleSummaryText(%q, %q) = %q, want %q", tt.vendor, tt.directive, got, tt.want)
		}
	}
}

// The left-hand filter has to narrow the results and survive doing it. Computed
// from the filtered results, ticking one class deleted every other checkbox from
// the panel, so there was no way back to a wider answer but editing the URL —
// which is what "the filter is not working" looked like from the browser.
func TestRuleFilterNarrowsWithoutLosingTheOtherCheckboxes(t *testing.T) {
	s, db := newTestServer(t)
	if _, err := db.CreateUser(t.Context(), "viewer", "a good long password", "viewer", "Viewer", false); err != nil {
		t.Fatal(err)
	}
	seedNginx(t, db, "web02", "10.90.4.11")
	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"viewer"}, "password": {"a good long password"}})

	const lookup = "/rules?url=shop.example.com/api/v2"
	classes := func(body string) []string {
		var out []string
		for _, m := range regexp.MustCompile(`name="class" value="([^"]+)"`).FindAllStringSubmatch(body, -1) {
			out = append(out, m[1])
		}
		return out
	}

	wide := c.get(lookup).Body.String()
	all := classes(wide)
	if len(all) < 2 {
		t.Fatalf("the fixture needs at least two action classes to filter between, got %v", all)
	}

	narrow := c.get(lookup + "&class=" + all[0]).Body.String()
	if got := classes(narrow); len(got) != len(all) {
		t.Errorf("filtering on %q left only %v in the panel, want all of %v", all[0], got, all)
	}
	if !regexp.MustCompile(`value="`+all[0]+`"\s*checked`).MatchString(narrow) {
		t.Errorf("class=%s came back unticked, so the panel disagrees with the URL", all[0])
	}

	// Narrowing must actually drop rules, or the checkbox is decoration.
	if strings.Count(narrow, "<tr") >= strings.Count(wide, "<tr") {
		t.Errorf("class=%s did not narrow the result table: %d rows filtered vs %d unfiltered",
			all[0], strings.Count(narrow, "<tr"), strings.Count(wide, "<tr"))
	}

	// A filter that matches nothing says so, rather than claiming nothing serves
	// the path, and still offers every checkbox back.
	none := c.get(lookup + "&vendor=haproxy").Body.String()
	if !strings.Contains(none, "No rules match the filters") {
		t.Errorf("a filter matching nothing did not say the filters are why")
	}
	if got := classes(none); len(got) != len(all) {
		t.Errorf("an empty filtered result left %v in the panel, want all of %v", got, all)
	}
}
