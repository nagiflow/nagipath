package api

import (
	"net/http/httptest"
	"testing"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
	"github.com/nagiflow/nagipath/internal/parse"
	"github.com/nagiflow/nagipath/internal/store"
	"github.com/nagiflow/nagipath/internal/trace"
	"google.golang.org/protobuf/encoding/protojson"
)

// TestRuleGroupingCollapses ports internal/web's old rules_test.go: identical
// rule sets on different nodes collapse into one group with count>1; a
// different rule set stays its own group.
func TestRuleGroupingCollapses(t *testing.T) {
	res1 := trace.LookupResult{
		Inst:  &trace.Inst{NodeName: "node1", Vendor: "nginx"},
		Rules: []trace.LookupRule{{Ordinal: 1, Rule: &trace.Rule{Directive: "proxy_pass", Args: "http://backend"}}},
	}
	res2 := trace.LookupResult{
		Inst:  &trace.Inst{NodeName: "node2", Vendor: "nginx"},
		Rules: []trace.LookupRule{{Ordinal: 1, Rule: &trace.Rule{Directive: "proxy_pass", Args: "http://backend"}}},
	}
	res3 := trace.LookupResult{
		Inst:  &trace.Inst{NodeName: "node3", Vendor: "nginx"},
		Rules: []trace.LookupRule{{Ordinal: 1, Rule: &trace.Rule{Directive: "rewrite", Args: "^/api /v1"}}},
	}

	groups := groupByNode([]trace.LookupResult{res1, res2, res3})
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	var collapsed *pb.RuleGroup
	for _, g := range groups {
		if g.Count > 1 {
			collapsed = g
		}
	}
	if collapsed == nil {
		t.Fatal("expected to find a collapsed group with count > 1")
	}
	if collapsed.Count != 2 || len(collapsed.Results) != 2 {
		t.Errorf("collapsed group = count %d, %d result(s), want 2 and 2", collapsed.Count, len(collapsed.Results))
	}
}

const testNginx = `
events {}
http {
  upstream app { server web02:8080; }
  server {
    listen 443 ssl;
    server_name shop.example.com;
    add_header X-Frame-Options DENY;
    location /api/ { proxy_pass http://app/; }
    location / { root /var/www; }
  }
}
`

// seedNginx is the same minimal fixture internal/web's old rules_test.go
// used: one node running one parsed nginx instance with two action classes
// (header, proxy), the smallest fixture the facet-widening test needs.
func seedNginx(t *testing.T, db *store.DB, host, addr string) int64 {
	t.Helper()
	ctx := t.Context()
	nodeID, err := db.AddNode(ctx, addr, 22, host, "nagipath", nil, nil, "manual", nil)
	if err != nil {
		t.Fatal(err)
	}
	instID, err := db.UpsertInstance(ctx, store.Instance{
		NodeID: nodeID, Vendor: "nginx", NaturalKey: "/etc/nginx/nginx.conf",
		DisplayName: host + " nginx", Version: "nginx/1.24.0",
		MainConfigPath: "/etc/nginx/nginx.conf", ServiceManager: "systemd", UnitName: "nginx.service",
	})
	if err != nil {
		t.Fatal(err)
	}
	colID, err := db.StartCollection(ctx, nodeID, "manual", nil)
	if err != nil {
		t.Fatal(err)
	}
	files := []parse.File{{Path: "/etc/nginx/nginx.conf", Content: []byte(testNginx)}}
	snapID, err := db.WriteSnapshot(ctx, store.Snapshot{
		InstanceID: instID, CollectionID: colID, CapturedAt: store.Now(),
		ConfigSource: "vendor_dump",
	}, []store.SnapshotFile{{Path: files[0].Path, Kind: "vendor_dump", Content: files[0].Content}})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.FinishCollection(ctx, colID, "succeeded", "", 1, 0, 1); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveDerived(ctx, snapID, instID, parse.NGINX(files, files[0].Path)); err != nil {
		t.Fatal(err)
	}
	return instID
}

// TestRuleFilterNarrowsWithoutLosingTheOtherCheckboxes ports the same-named
// test from internal/web's old rules_test.go: ticking one class facet must
// narrow the results but must not delete the other checkboxes from the
// panel — the facet counts are computed from the widened query, not the
// filtered one, so there is always a way back to "everything".
func TestRuleFilterNarrowsWithoutLosingTheOtherCheckboxes(t *testing.T) {
	db := testDB(t)
	seedNginx(t, db, "web02", "10.90.4.11")
	s := New(db, nil, false)

	const lookup = "/rules?url=shop.example.com/api/v2"

	w := httptest.NewRecorder()
	s.getRules(w, httptest.NewRequest("GET", lookup, nil))
	var wide pb.RulesResponse
	if err := protojson.Unmarshal(w.Body.Bytes(), &wide); err != nil {
		t.Fatal(err)
	}
	if len(wide.ClassFacets) < 2 {
		t.Fatalf("the fixture needs at least two action classes to filter between, got %+v", wide.ClassFacets)
	}
	firstClass := wide.ClassFacets[0].Value

	w = httptest.NewRecorder()
	s.getRules(w, httptest.NewRequest("GET", lookup+"&class="+firstClass, nil))
	var narrow pb.RulesResponse
	if err := protojson.Unmarshal(w.Body.Bytes(), &narrow); err != nil {
		t.Fatal(err)
	}
	if len(narrow.ClassFacets) != len(wide.ClassFacets) {
		t.Errorf("filtering on %q left only %d facet(s) in the panel, want all %d", firstClass, len(narrow.ClassFacets), len(wide.ClassFacets))
	}
	found := false
	for _, c := range narrow.ClassesSelected {
		if c == firstClass {
			found = true
		}
	}
	if !found {
		t.Errorf("class=%s came back not selected, so the panel disagrees with the URL", firstClass)
	}
	if narrow.Rules >= wide.Rules {
		t.Errorf("class=%s did not narrow the result set: %d rules filtered vs %d unfiltered", firstClass, narrow.Rules, wide.Rules)
	}

	// A filter that matches nothing says so via Rules=0 with Groups empty,
	// and still offers every checkbox back (RulesUnfiltered/ClassFacets stay
	// computed from the widened query).
	w = httptest.NewRecorder()
	s.getRules(w, httptest.NewRequest("GET", lookup+"&vendor=haproxy", nil))
	var none pb.RulesResponse
	if err := protojson.Unmarshal(w.Body.Bytes(), &none); err != nil {
		t.Fatal(err)
	}
	if none.Rules != 0 || len(none.Groups) != 0 {
		t.Errorf("vendor=haproxy should match nothing in an all-nginx fixture, got rules=%d groups=%d", none.Rules, len(none.Groups))
	}
	if len(none.ClassFacets) != len(wide.ClassFacets) {
		t.Errorf("an empty filtered result left %d facet(s) in the panel, want all %d", len(none.ClassFacets), len(wide.ClassFacets))
	}
}
