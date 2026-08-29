package api

import (
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
	"github.com/nagiflow/nagipath/internal/trace"
	"google.golang.org/protobuf/encoding/protojson"
)

// TestTraceProvenancePanelShowsTheRealLines ports internal/web's old
// TestTraceProvenancePanelShowsTheRealLines: the Provenance panel has to
// show real bytes out of the stored snapshot — not just a link that says it
// could — and every fired rule has to be selectable into it. It also has to
// keep working when the selected rule has no file recorded, which is a
// different branch of the same panel.
func TestTraceProvenancePanelShowsTheRealLines(t *testing.T) {
	db := testDB(t)
	seedNginx(t, db, "web02", "10.90.4.11")
	s := New(db, nil, false)

	w := httptest.NewRecorder()
	s.getTrace(w, httptest.NewRequest("GET", "/trace?url=https://shop.example.com/api/v2", nil))
	if w.Code != 200 {
		t.Fatalf("GET /trace = %d\n%s", w.Code, w.Body.String())
	}
	var resp pb.TraceResponse
	if err := protojson.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Prov == nil {
		t.Fatal("/trace returned no Provenance, so no rule on the page can be checked without leaving it")
	}
	// A line out of the fixture. The panel is worthless if it shows the rule
	// text it already showed in the hop's fired-rules list.
	if resp.Prov.Path != "/etc/nginx/nginx.conf" {
		t.Errorf("Provenance.Path = %q, want the nginx config", resp.Prov.Path)
	}
	found := false
	for _, l := range resp.Prov.Lines {
		if strings.Contains(l.Text, "proxy_pass") {
			found = true
		}
	}
	if !found || len(resp.Prov.Lines) == 0 {
		t.Errorf("Provenance excerpt is missing the proxy_pass source line: %+v", resp.Prov.Lines)
	}

	// Every fired rule is selectable into the panel by its rule id.
	var ruleID int64
	for _, h := range resp.Hops {
		if len(h.FiredRules) > 0 {
			ruleID = h.FiredRules[0].RuleId
		}
	}
	if ruleID == 0 {
		t.Fatal("no hop has a fired rule to select into the provenance panel")
	}
	w = httptest.NewRecorder()
	s.getTrace(w, httptest.NewRequest("GET", "/trace?url=https://shop.example.com/api/v2&prov="+strconv.FormatInt(ruleID, 10), nil))
	var picked pb.TraceResponse
	if err := protojson.Unmarshal(w.Body.Bytes(), &picked); err != nil {
		t.Fatal(err)
	}
	if !picked.ProvPicked || picked.Prov == nil || picked.Prov.RuleId != ruleID {
		t.Errorf("selecting rule %d did not come back as the picked provenance: %+v", ruleID, picked.Prov)
	}

	// The no-file branch: a rule parsed before provenance was recorded says so
	// rather than returning an empty panel or a link to nowhere.
	v := s.traceProv(t.Context(), &trace.Trace{Hops: []*trace.Hop{{
		Rules: []trace.HopRule{{Scope: "route", Rule: &trace.Rule{ID: 9, Directive: "return", Args: "404"}}},
	}}}, 0)
	if v == nil || v.Missing == "" || len(v.Lines) > 0 {
		t.Errorf("a rule with no file produced %+v, want a stated reason and no excerpt", v)
	}
}
