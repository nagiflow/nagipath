package api

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
	"github.com/nagiflow/nagipath/internal/store"
	"google.golang.org/protobuf/encoding/protojson"
)

// seedProbes ports internal/web's old seedProbes fixture: three probes (one
// recent, one 40 days old, one that never got a response) plus evidence
// across three hosts so state-change formatting has something real to prove.
func seedProbes(t *testing.T, db *store.DB, actor int64) (newest, older int64) {
	t.Helper()
	ctx := t.Context()
	ins := func(token, u, at string, status any, result string) int64 {
		res, err := db.W.ExecContext(ctx, `INSERT INTO probe (actor_user_id, method, url,
			correlation_token, origin_host, requested_at, status_code, duration_ms, result)
			VALUES (?, 'GET', ?, ?, 'nagipath-1', ?, ?, 42, ?)`,
			actor, u, token, at, status, result)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		return id
	}
	now := time.Now().UTC()
	newest = ins("f2c9d1a4b7", "https://shop.example.com/api/v2", now.Format(time.RFC3339), 200, "completed")
	older = ins("aa11bb22cc", "https://shop.example.com/api/v2", now.AddDate(0, 0, -40).Format(time.RFC3339), 200, "completed")
	ins("dd33ee44ff", "https://admin.example.com/", now.Format(time.RFC3339), nil, "failed")

	host := func(name, addr string) int64 {
		nodeID, err := db.AddNode(ctx, addr, 22, name, "nagipath", nil, nil, "manual", nil)
		if err != nil {
			t.Fatal(err)
		}
		instID, err := db.UpsertInstance(ctx, store.Instance{
			NodeID: nodeID, Vendor: "nginx", NaturalKey: "/etc/nginx/nginx.conf",
			DisplayName: name + " nginx", MainConfigPath: "/etc/nginx/nginx.conf",
		})
		if err != nil {
			t.Fatal(err)
		}
		return instID
	}
	web02, web05, app01 := host("web02", "10.90.9.11"), host("web05", "10.90.9.12"), host("app01", "10.90.9.13")
	for _, e := range []struct {
		inst                     int64
		kind, grants, prior, log string
	}{
		{web02, "access_log_line", "verified", "inferred", "/var/log/nginx/access.log"},
		{web02, "header_absent", "disproved", "inferred", ""},
		{web05, "response_header", "observed_effect", "degraded", ""},
		{app01, "header_absent", "disproved", "inferred", ""},
	} {
		if _, err := db.W.ExecContext(ctx, `INSERT INTO probe_evidence (probe_id, instance_id,
			kind, raw_evidence, log_path, parsed_fields, grants, observed_at)
			VALUES (?, ?, ?, 'evidence bytes', ?, json_object('prior_confidence', ?), ?, ?)`,
			newest, e.inst, e.kind, e.log, e.prior, e.grants, now.Format(time.RFC3339)); err != nil {
			t.Fatal(err)
		}
	}
	return newest, older
}

// TestProbeHistoryListsFiltersAndSelects ports internal/web's old
// TestProbeHistoryListsFiltersAndSelects: all three filter controls have to
// reach the query, the fleet-wide list has to actually list the fleet, a
// row selects itself into the detail panel, and the export is the filtered
// set (and is audited).
func TestProbeHistoryListsFiltersAndSelects(t *testing.T) {
	db := testDB(t)
	ctx := t.Context()
	actor, err := db.CreateUser(ctx, "admin", "a good long password", "admin", "Admin", false)
	if err != nil {
		t.Fatal(err)
	}
	newest, older := seedProbes(t, db, actor)
	s := New(db, nil, false)
	adminUser := store.User{ID: actor, Role: "admin"}
	get := func(path string) *pb.ProbeHistoryResponse {
		t.Helper()
		w := httptest.NewRecorder()
		s.getProbeHistory(w, httptest.NewRequest("GET", path, nil))
		var resp pb.ProbeHistoryResponse
		if err := protojson.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		return &resp
	}
	hasToken := func(resp *pb.ProbeHistoryResponse, token string) bool {
		for _, p := range resp.Probes {
			if strings.HasPrefix(p.Token, token) {
				return true
			}
		}
		return false
	}

	// Fleet-wide: no url at all, which is the screen the nav reaches.
	all := get("/trace/history")
	for _, want := range []string{"f2c9d1", "dd33ee"} {
		if !hasToken(all, want) {
			t.Errorf("the fleet-wide list is missing token %q", want)
		}
	}
	// The 40-day-old probe is outside the default 7-day window.
	if hasToken(all, "aa11bb") {
		t.Error("the default 7-day range listed a probe from 40 days ago")
	}
	if wide := get("/trace/history?range=all"); !hasToken(wide, "aa11bb") {
		t.Error("Range: all still hid the 40-day-old probe")
	}

	// The free-text box filters on entry point, actor and token.
	if q := get("/trace/history?range=all&q=admin.example.com"); !hasToken(q, "dd33ee") || hasToken(q, "f2c9d1") {
		t.Error("the free-text filter did not narrow the list to the entry point asked for")
	}
	if tok := get("/trace/history?range=all&q=aa11bb"); !hasToken(tok, "aa11bb") {
		t.Error("a probe id typed into the filter did not find its probe")
	}

	// Selecting a row loads that probe, not whichever one was newest at its URL.
	sel := get(fmt.Sprintf("/trace/history?range=all&probe=%d", older))
	if sel.Selected == nil || !strings.HasPrefix(sel.Selected.Token, "aa11bb") {
		t.Errorf("selecting probe %d did not select it: %+v", older, sel.Selected)
	}

	// The newest probe's evidence, with the state change it granted: a verified
	// row and an observed one are different answers, and the row that granted
	// nothing (app01) is still listed rather than dropped.
	det := get(fmt.Sprintf("/trace/history?probe=%d", newest))
	if det.Selected == nil {
		t.Fatal("selecting the newest probe returned no detail")
	}
	if det.Selected.ChangeCount != 2 {
		t.Errorf("ChangeCount = %d, want 2 (web02 verified, web05 observed)", det.Selected.ChangeCount)
	}
	byHost := map[string]*pb.HopChangePB{}
	for _, c := range det.Selected.Changes {
		byHost[c.Host] = c
	}
	if c := byHost["web02"]; c == nil || c.Prior != "inferred" || c.To != "verified" {
		t.Errorf("web02's change = %+v, want inferred -> verified", c)
	}
	if c := byHost["web05"]; c == nil || c.Prior != "degraded" || c.To != "observed" {
		t.Errorf("web05's change = %+v, want degraded -> observed", c)
	}
	if c := byHost["app01"]; c == nil || c.To != "" {
		t.Errorf("app01 raised nothing and should still be listed with To empty, got %+v", c)
	}
	if det.Selected.LogReads == "" {
		t.Error("LogReads is empty despite one evidence row carrying a log path")
	}

	// Actor and outcome are two more controls that have to reach the query.
	if got := get("/trace/history?range=all&actor=admin"); !hasToken(got, "f2c9d1") {
		t.Error("filtering by actor lost the probes that actor sent")
	}
	if got := get("/trace/history?range=all&actor=nobody"); hasToken(got, "f2c9d1") {
		t.Error("filtering by an actor who sent nothing still listed probes")
	}
	fail := get("/trace/history?range=all&outcome=failed")
	if !hasToken(fail, "dd33ee") || hasToken(fail, "f2c9d1") {
		t.Error("the outcome filter did not narrow the list to failed probes")
	}

	// The export is the filter, all of it, and it is audited because it leaves
	// the product. Written down as one row per probe, header included.
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/trace/history?range=all&outcome=failed&export=csv", nil)
	req = req.WithContext(context.WithValue(req.Context(), userKey, adminUser))
	s.getProbeHistory(w, req)
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Fatalf("export Content-Type = %q, not text/csv", ct)
	}
	csvBody := w.Body.String()
	if !strings.Contains(csvBody, "dd33ee44ff") || strings.Contains(csvBody, "f2c9d1a4b7") {
		t.Errorf("the export is not the filtered set:\n%s", csvBody)
	}
	if n := strings.Count(strings.TrimSpace(csvBody), "\n"); n != 1 {
		t.Errorf("the export has %d data rows, want 1", n)
	}
	events, err := db.AuditEvents(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	audited := false
	for _, e := range events {
		if e.Action == "export.csv" {
			audited = true
		}
	}
	if !audited {
		t.Error("an export left the product without being audited")
	}

	// A probe with no response says so via Status=0, not a fake status.
	none := get("/trace/history?q=admin.example.com")
	if len(none.Probes) == 0 || none.Probes[0].Status != 0 {
		t.Errorf("the no-response probe should have Status=0, got %+v", none.Probes)
	}

	// One entry point's own history does not leak another entry point's probes.
	one := get("/trace/history?url=" + "https://admin.example.com/")
	if hasToken(one, "f2c9d1") {
		t.Error("one entry point's history listed another entry point's probes")
	}
}

// TestProbeHistoryEmptyStatesAreDistinct ports internal/web's old test of
// the same name: nothing configured, filtered to nothing, and no probe at
// this particular entry point are three different facts and the response
// must not conflate them.
func TestProbeHistoryEmptyStatesAreDistinct(t *testing.T) {
	db := testDB(t)
	ctx := t.Context()
	actor, err := db.CreateUser(ctx, "admin", "a good long password", "admin", "Admin", false)
	if err != nil {
		t.Fatal(err)
	}
	s := New(db, nil, false)

	w := httptest.NewRecorder()
	s.getProbeHistory(w, httptest.NewRequest("GET", "/trace/history?range=all", nil))
	var empty pb.ProbeHistoryResponse
	if err := protojson.Unmarshal(w.Body.Bytes(), &empty); err != nil {
		t.Fatal(err)
	}
	if empty.Total != 0 || len(empty.Probes) != 0 {
		t.Error("with no probes at all the response should be empty")
	}

	seedProbes(t, db, actor)

	w = httptest.NewRecorder()
	s.getProbeHistory(w, httptest.NewRequest("GET", "/trace/history?range=all&q=nothing-matches-this", nil))
	var filtered pb.ProbeHistoryResponse
	if err := protojson.Unmarshal(w.Body.Bytes(), &filtered); err != nil {
		t.Fatal(err)
	}
	if filtered.Total != 0 {
		t.Error("a filter that matched nothing should report Total=0, distinguishable from no probes ever sent")
	}

	w = httptest.NewRecorder()
	s.getProbeHistory(w, httptest.NewRequest("GET", "/trace/history?range=all&url=https://legacy.example.com/", nil))
	var scoped pb.ProbeHistoryResponse
	if err := protojson.Unmarshal(w.Body.Bytes(), &scoped); err != nil {
		t.Fatal(err)
	}
	if scoped.Total != 0 || scoped.Url != "https://legacy.example.com/" {
		t.Errorf("an entry point with no probes: total=%d url=%q", scoped.Total, scoped.Url)
	}
}

// TestProbeHistoryPagesRatherThanTruncating ports internal/web's old test
// of the same name: the response states which slice of the filtered set
// this page is, and a cursor reaches the rest — the earlier version
// silently kept the newest 200 and said nothing.
func TestProbeHistoryPagesRatherThanTruncating(t *testing.T) {
	db := testDB(t)
	ctx := t.Context()
	actor, err := db.CreateUser(ctx, "admin", "a good long password", "admin", "Admin", false)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for i := range probesPageSize + 10 {
		if _, err := db.W.ExecContext(ctx, `INSERT INTO probe (actor_user_id, method, url,
			correlation_token, origin_host, requested_at, status_code, result)
			VALUES (?, 'GET', 'https://shop.example.com/api/v2', ?, 'nagipath-1', ?, 200, 'completed')`,
			actor, fmt.Sprintf("p%05d", i),
			now.Add(-time.Duration(i)*time.Minute).Format(time.RFC3339)); err != nil {
			t.Fatal(err)
		}
	}
	s := New(db, nil, false)

	w := httptest.NewRecorder()
	s.getProbeHistory(w, httptest.NewRequest("GET", "/trace/history", nil))
	var first pb.ProbeHistoryResponse
	if err := protojson.Unmarshal(w.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	if first.From != 1 || first.To != int32(probesPageSize) || first.Total != int32(probesPageSize+10) {
		t.Errorf("first page = from %d to %d of %d, want 1 to %d of %d", first.From, first.To, first.Total, probesPageSize, probesPageSize+10)
	}
	if !first.HasMore || first.NextCursor == "" {
		t.Fatal("with more rows than a page there is no way to reach the older ones")
	}

	w = httptest.NewRecorder()
	s.getProbeHistory(w, httptest.NewRequest("GET", "/trace/history?cursor="+first.NextCursor, nil))
	var next pb.ProbeHistoryResponse
	if err := protojson.Unmarshal(w.Body.Bytes(), &next); err != nil {
		t.Fatal(err)
	}
	if next.From != int32(probesPageSize+1) || next.To != int32(probesPageSize+10) {
		t.Errorf("second page = from %d to %d, want %d to %d", next.From, next.To, probesPageSize+1, probesPageSize+10)
	}
	// The oldest probe is only on page two, which is the point of the control.
	oldest := fmt.Sprintf("p%05d", probesPageSize+9)
	firstHas, nextHas := false, false
	for _, p := range first.Probes {
		if p.Token == oldest {
			firstHas = true
		}
	}
	for _, p := range next.Probes {
		if p.Token == oldest {
			nextHas = true
		}
	}
	if !nextHas || firstHas {
		t.Error("the older page did not carry the rows the first page left out")
	}
}
