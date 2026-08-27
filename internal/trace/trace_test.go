package trace

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nagiflow/nagipath/internal/parse"
	"github.com/nagiflow/nagipath/internal/store"
)

// The fixtures below build the database directly rather than through the
// collector: the walk is being tested, not the capture, and a parse.Result is the
// exact contract between them.

func testDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "trace.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// seed installs one Instance with the given config text, parsed by the real
// parser, so the test cannot drift from what the parser actually emits.
func seed(t *testing.T, db *store.DB, node, addr, vendor, path, body string) int64 {
	t.Helper()
	return seedFiles(t, db, node, addr, vendor, path, []parse.File{{Path: path, Content: []byte(body)}})
}

// seedFiles is seed for an Instance whose configuration is a tree rather than one
// file. The include resolution is the parser's, not the test's, so a lab config
// that moves a directive into a new file still has to end up in the same place.
func seedFiles(t *testing.T, db *store.DB, node, addr, vendor, path string, files []parse.File) int64 {
	t.Helper()
	ctx := context.Background()
	nodeID, err := db.AddNode(ctx, addr, 22, node, "nagipath", nil, nil, "manual", nil)
	if err != nil {
		t.Fatal(err)
	}
	instID, err := db.UpsertInstance(ctx, store.Instance{
		NodeID: nodeID, Vendor: vendor, NaturalKey: path, DisplayName: node + "/" + vendor,
		Version: vendor + "/test", MainConfigPath: path, ServiceManager: "container",
	})
	if err != nil {
		t.Fatal(err)
	}
	colID, err := db.StartCollection(ctx, nodeID, "manual", nil)
	if err != nil {
		t.Fatal(err)
	}
	snapFiles := make([]store.SnapshotFile, len(files))
	for i, f := range files {
		snapFiles[i] = store.SnapshotFile{Path: f.Path, Kind: "config_file", Content: f.Content}
	}
	snapID, err := db.WriteSnapshot(ctx, store.Snapshot{
		InstanceID: instID, CollectionID: colID, CapturedAt: store.Now(),
		ConfigSource: "vendor_dump",
	}, snapFiles)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.FinishCollection(ctx, colID, "succeeded", "", 1, 0, 1); err != nil {
		t.Fatal(err)
	}

	var res *parse.Result
	switch vendor {
	case "nginx":
		res = parse.NGINX(files, path)
	case "apache":
		res = parse.Apache(files, path)
	default:
		res = parse.HAProxy(files)
	}
	if err := db.SaveDerived(ctx, snapID, instID, res); err != nil {
		t.Fatal(err)
	}
	return nodeID
}

const edge = `
events {}
http {
  upstream app {
    server web02:8080;
  }
  server {
    listen 443 ssl;
    server_name shop.example.com;
    add_header X-Frame-Options DENY;
    location /api/ {
      proxy_pass http://app/;
    }
    location = /health {
      return 200;
    }
    location ~ \.php$ {
      proxy_pass http://app;
    }
    location / {
      root /var/www;
      add_header X-Route inner;
    }
  }
}
`

const backend = `
events {}
http {
  server {
    listen 8080;
    server_name shop.example.com;
    location / {
      proxy_pass http://payments.external.example:9000;
    }
  }
}
`

func load(t *testing.T, db *store.DB) *Topology {
	t.Helper()
	top, err := Load(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	return top
}

func TestWalkCrossesNodesAndRewritesPath(t *testing.T) {
	db := testDB(t)
	seed(t, db, "lb01", "10.90.4.2", "nginx", "/etc/nginx/nginx.conf", edge)
	seed(t, db, "web02", "10.90.4.3", "nginx", "/etc/nginx/nginx.conf", backend)

	tr := Walk(load(t, db), Query{Scheme: "https", Hostname: "shop.example.com", Path: "/api/v2/charge"})

	if len(tr.Hops) < 3 {
		t.Fatalf("want at least 3 hops (edge, backend, external), got %d", len(tr.Hops))
	}
	if tr.Hops[0].Inst.NodeName != "lb01" {
		t.Errorf("entry hop = %s, want lb01", tr.Hops[0].Inst.NodeName)
	}
	// proxy_pass http://app/ has a URI component, so nginx strips the matched
	// location prefix. This is the claim the whole product hangs on.
	if got := tr.Hops[0].EffectivePath; got != "/v2/charge" {
		t.Errorf("effective path at the edge = %q, want /v2/charge", got)
	}
	if len(tr.Hops[0].PathChangedBy) == 0 {
		t.Error("the path changed but no rule was named as responsible")
	}
	if tr.Hops[1].Inst == nil || tr.Hops[1].Inst.NodeName != "web02" {
		t.Fatalf("hop 1 = %+v, want web02", tr.Hops[1])
	}
	if tr.Hops[1].InboundPath != "/v2/charge" {
		t.Errorf("web02 received %q, want /v2/charge", tr.Hops[1].InboundPath)
	}
	if tr.TerminalReason != TermExternal {
		t.Errorf("terminal reason = %q, want external_hop", tr.TerminalReason)
	}
	last := tr.Hops[len(tr.Hops)-1]
	if !last.IsExternal || last.ExternalTarget != "payments.external.example:9000" {
		t.Errorf("last hop = %+v", last)
	}
	if last.ExternalReason == "" {
		t.Error("an external hop must say why it is external")
	}
}

const edgeA = `
events {}
http {
  upstream app {
    server backend.internal:8080;
  }
  server {
    listen 443 ssl;
    server_name shop-a.example.com;
    location /api/ {
      proxy_pass http://app;
    }
  }
}
`

const edgeB = `
events {}
http {
  upstream app {
    server backend.internal:8080;
  }
  server {
    listen 443 ssl;
    server_name shop-b.example.com;
    location /api/ {
      proxy_pass http://app;
    }
  }
}
`

func targetBody(serverName string) string {
	return `
events {}
http {
  server {
    listen 8080;
    server_name ` + serverName + `;
    location / {
      return 200;
    }
  }
}
`
}

// TestWalkResolvesUpstreamHostnamesOnTheirOwnNode guards ADR-0010: split-horizon
// DNS is the normal case in a fleet, so the same name legitimately means a
// different address on different Nodes, and the only resolution that is ever
// valid for an Upstream Member is the one performed on the Node whose
// configuration names it — never another Node's answer for the same name,
// silently picked because it happened to be collected more recently.
func TestWalkResolvesUpstreamHostnamesOnTheirOwnNode(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	edgeAID := seed(t, db, "edgeA", "10.0.1.1", "nginx", "/etc/nginx/nginx.conf", edgeA)
	edgeBID := seed(t, db, "edgeB", "10.0.1.2", "nginx", "/etc/nginx/nginx.conf", edgeB)
	seed(t, db, "targetA", "10.0.2.10", "nginx", "/etc/nginx/nginx.conf", targetBody("shop-a.example.com"))
	seed(t, db, "targetB", "10.0.2.20", "nginx", "/etc/nginx/nginx.conf", targetBody("shop-b.example.com"))

	// Both edges proxy to the literal name "backend.internal", but each Node's own
	// getent hosts answer points at a different real Instance.
	if err := db.SaveDNS(ctx, edgeAID, "backend.internal", []string{"10.0.2.10"}, "getent_hosts"); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveDNS(ctx, edgeBID, "backend.internal", []string{"10.0.2.20"}, "getent_hosts"); err != nil {
		t.Fatal(err)
	}

	top := load(t, db)

	trA := Walk(top, Query{Scheme: "https", Hostname: "shop-a.example.com", Path: "/api/"})
	if len(trA.Hops) < 2 || trA.Hops[1].Inst == nil || trA.Hops[1].Inst.NodeName != "targetA" {
		t.Fatalf("edgeA's trace hop 1 = %+v, want targetA (edgeA's own DNS view)", trA.Hops)
	}

	trB := Walk(top, Query{Scheme: "https", Hostname: "shop-b.example.com", Path: "/api/"})
	if len(trB.Hops) < 2 || trB.Hops[1].Inst == nil || trB.Hops[1].Inst.NodeName != "targetB" {
		t.Fatalf("edgeB's trace hop 1 = %+v, want targetB (edgeB's own DNS view)", trB.Hops)
	}
}

func TestWalkPicksTheEdgeNotTheBackendAsEntry(t *testing.T) {
	db := testDB(t)
	seed(t, db, "lb01", "10.90.4.2", "nginx", "/etc/nginx/nginx.conf", edge)
	seed(t, db, "web02", "10.90.4.3", "nginx", "/etc/nginx/nginx.conf", backend)

	// Both Instances claim shop.example.com, but only on their own port. Asking on
	// 443 must not offer the backend as an entry at all.
	tr := Walk(load(t, db), Query{Scheme: "https", Hostname: "shop.example.com", Path: "/"})
	if len(tr.EntryCandidates) != 1 {
		t.Fatalf("want 1 candidate on port 443, got %d", len(tr.EntryCandidates))
	}
	if !tr.EntryCandidates[0].Selected || tr.EntryCandidates[0].Inst.NodeName != "lb01" {
		t.Errorf("candidate = %+v", tr.EntryCandidates[0])
	}
	if tr.EntryCandidates[0].MatchedBy != "exact name shop.example.com" {
		t.Errorf("matched by %q", tr.EntryCandidates[0].MatchedBy)
	}
}

func TestWalkNginxMatchOrder(t *testing.T) {
	db := testDB(t)
	seed(t, db, "lb01", "10.90.4.2", "nginx", "/etc/nginx/nginx.conf", edge)
	top := load(t, db)

	cases := []struct{ path, want string }{
		{"/health", "= /health"},   // exact beats everything
		{"/index.php", `~ \.php$`}, // regex beats the longest prefix /
		{"/static/x", "/"},         // longest prefix when nothing else matches
	}
	for _, c := range cases {
		tr := Walk(top, Query{Scheme: "https", Hostname: "shop.example.com", Path: c.path})
		if tr.Hops[0].Route == nil {
			t.Fatalf("%s: no route selected (%s)", c.path, tr.TerminalReason)
		}
		if tr.Hops[0].Precedence == "" {
			t.Errorf("%s: route selected with no explanation", c.path)
		}
		got := tr.Hops[0].Route
		switch c.want {
		case "= /health":
			if got.MatchType != "exact" {
				t.Errorf("%s selected %s %s", c.path, got.MatchType, got.Pattern)
			}
		case `~ \.php$`:
			if got.MatchType != "regex" {
				t.Errorf("%s selected %s %s, want the regex location", c.path, got.MatchType, got.Pattern)
			}
		case "/":
			if got.Pattern != "/" {
				t.Errorf("%s selected %s, want /", c.path, got.Pattern)
			}
		}
	}
}

// The add_header trap: a location declaring its own add_header discards every
// inherited one. Reporting the inherited header as applied would be a security
// claim that is simply false.
func TestWalkReportsShadowedHeaders(t *testing.T) {
	db := testDB(t)
	seed(t, db, "lb01", "10.90.4.2", "nginx", "/etc/nginx/nginx.conf", edge)

	tr := Walk(load(t, db), Query{Scheme: "https", Hostname: "shop.example.com", Path: "/static/x"})
	hop := tr.Hops[0]
	var shadowed bool
	for _, r := range hop.Shadowed {
		if r.Rule.Directive == "add_header" && r.Rule.ShadowedBy != "" {
			shadowed = true
		}
	}
	if !shadowed {
		t.Errorf("server-level add_header should be shadowed by the location's own; shadowed = %+v", hop.Shadowed)
	}
	for _, r := range hop.Rules {
		if r.Rule.Directive == "add_header" && r.Scope == "site" {
			t.Error("an inherited add_header was reported as applied")
		}
	}
}

// A RewriteRule guarded by RewriteCond is a branch, not a path change. The
// conditions test the request — a Host header, a method — and a snapshot has
// neither. Applying the rule anyway reports a path only some requests take as the
// one this request takes.
func TestWalkApacheRewriteCondIsABranch(t *testing.T) {
	db := testDB(t)
	seed(t, db, "app01", "10.90.4.30", "apache", "/etc/httpd/conf/httpd.conf", `
Listen 8080
<VirtualHost *:8080>
    ServerName app.example.com
    DocumentRoot /var/www
    RewriteEngine On
    RewriteCond %{HTTP_HOST} ^internal\.example\.com$
    RewriteRule ^/(.*)$ /internal/$1 [L]
    RewriteRule ^/old/(.*)$ /new/$1 [L]
    <Location "/">
        Require all granted
    </Location>
</VirtualHost>
`)
	tr := Walk(load(t, db), Query{Scheme: "http", Hostname: "app.example.com", Path: "/charge", Port: 8080})
	hop := tr.Hops[0]
	if hop.EffectivePath != "/charge" {
		t.Errorf("effective path = %q, want /charge unchanged: the rewrite is conditional",
			hop.EffectivePath)
	}
	var raws []string
	for _, b := range tr.Undetermined {
		raws = append(raws, b.Raw+" — "+b.Reason)
	}
	joined := strings.Join(raws, "\n")
	if !strings.Contains(joined, "/internal/") {
		t.Errorf("the guarded rewrite is not reported as undetermined; got:\n%s", joined)
	}
	if !strings.Contains(joined, "RewriteCond") {
		t.Errorf("an undetermined rewrite must say the condition is what makes it so; got:\n%s", joined)
	}
	// The unguarded rule below it does not match /charge, so nothing else fired —
	// and the [L] on the conditional rule must not have stopped the walk either.
	if len(hop.PathChangedBy) != 0 {
		t.Errorf("no rule should have changed the path; got %+v", hop.PathChangedBy)
	}
}

// A regex container's Specificity is deliberately 0 (parse/apache.go): a regex
// has no inherent narrower/wider ordering, so real Apache resolves a tie between
// two same-rank LocationMatch/DirectoryMatch sections by config order — the last
// one to merge wins — never by which regex source string happens to be longer.
func TestWalkApacheRegexContainerTieBreaksByConfigOrderNotPatternLength(t *testing.T) {
	// r1 is declared first but has the textually longer pattern; r2 is declared
	// second with a shorter one. A pattern-length tie-break (the bug) would keep
	// r1; config order (the fix) must pick r2, since it merges last.
	r1 := &Route{ID: 1, MatchType: "location_match", Pattern: `^/api/(charge|refund|void)$`,
		PrecedenceRank: parse.RankApacheLocation}
	r2 := &Route{ID: 2, MatchType: "location_match", Pattern: `^/api/charge$`,
		PrecedenceRank: parse.RankApacheLocation}

	best, _, branches := apacheRoute([]*Route{r1, r2}, "/api/charge")
	if len(branches) != 0 {
		t.Fatalf("unexpected branches: %+v", branches)
	}
	if best == nil || best.ID != r2.ID {
		t.Errorf("apacheRoute picked route %+v, want r2 (the later, config-order winner)", best)
	}
}

func TestWalkTerminalReasons(t *testing.T) {
	db := testDB(t)
	seed(t, db, "lb01", "10.90.4.2", "nginx", "/etc/nginx/nginx.conf", edge)
	top := load(t, db)

	cases := []struct {
		name string
		q    Query
		want string
	}{
		{"static content", Query{Scheme: "https", Hostname: "shop.example.com", Path: "/static/x"}, TermStatic},
		{"no listener", Query{Scheme: "http", Hostname: "shop.example.com", Path: "/", Port: 8081}, TermNoListener},
		{"no site", Query{Scheme: "https", Hostname: "nobody.example.com", Path: "/"}, TermNoSite},
	}
	for _, c := range cases {
		tr := Walk(top, c.q)
		if tr.TerminalReason != c.want {
			t.Errorf("%s: terminal reason = %q, want %q", c.name, tr.TerminalReason, c.want)
		}
	}
}

func TestWalkAlwaysExplainsWhyItStopped(t *testing.T) {
	db := testDB(t)
	seed(t, db, "lb01", "10.90.4.2", "nginx", "/etc/nginx/nginx.conf", edge)
	top := load(t, db)
	for _, path := range []string{"/", "/api/x", "/health", "/x.php", "/deep/nested/thing"} {
		tr := Walk(top, Query{Scheme: "https", Hostname: "shop.example.com", Path: path})
		if tr.TerminalReason == "" {
			t.Errorf("%s produced a trace with no terminal reason", path)
		}
	}
}

func TestSaveRoundTrips(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	seed(t, db, "lb01", "10.90.4.2", "nginx", "/etc/nginx/nginx.conf", edge)
	seed(t, db, "web02", "10.90.4.3", "nginx", "/etc/nginx/nginx.conf", backend)

	tr := Walk(load(t, db), Query{Scheme: "https", Hostname: "shop.example.com", Path: "/api/v2/charge"})
	id, err := Save(ctx, db, tr, nil)
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	var hops, rules int
	db.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM hop WHERE trace_id = ?`, id).Scan(&hops)
	db.R.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM hop_rule hr JOIN hop h ON h.id = hr.hop_id WHERE h.trace_id = ?`,
		id).Scan(&rules)
	if hops != len(tr.Hops) {
		t.Errorf("stored %d hops, computed %d", hops, len(tr.Hops))
	}
	if rules == 0 {
		t.Error("no hop_rule rows stored")
	}

	recent, err := Recent(ctx, db, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 1 || recent[0].Hostname != "shop.example.com" {
		t.Errorf("recent = %+v", recent)
	}
	if recent[0].TerminalReason != tr.TerminalReason {
		t.Errorf("stored terminal reason = %q", recent[0].TerminalReason)
	}

	// The same URL traced again is the same entry point, and the list is one row per
	// entry point showing the newest run. Both the Dashboard panel and the Trace
	// screen's history read this, and both were printing one URL over and over.
	again, err := Save(ctx, db, Walk(load(t, db),
		Query{Scheme: "https", Hostname: "shop.example.com", Path: "/api/v2/charge"}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if recent, err = Recent(ctx, db, 10); err != nil {
		t.Fatal(err)
	}
	if len(recent) != 1 {
		t.Fatalf("tracing one URL twice listed %d entry points", len(recent))
	}
	if recent[0].ID != again {
		t.Errorf("the listed run is trace %d, not the newest one (%d)", recent[0].ID, again)
	}
	// A different path is a different entry point and gets its own row.
	if _, err := Save(ctx, db, Walk(load(t, db),
		Query{Scheme: "https", Hostname: "shop.example.com", Path: "/static/app.js"}), nil); err != nil {
		t.Fatal(err)
	}
	if recent, err = Recent(ctx, db, 10); err != nil {
		t.Fatal(err)
	}
	if len(recent) != 2 {
		t.Errorf("two different entry points listed %d rows", len(recent))
	}
}

func TestWalkWithEmptyFleetSaysSo(t *testing.T) {
	db := testDB(t)
	tr := Walk(load(t, db), Query{Scheme: "https", Hostname: "shop.example.com", Path: "/"})
	if tr.TerminalReason != TermNoListener {
		t.Errorf("terminal reason = %q", tr.TerminalReason)
	}
	if len(tr.Notes) == 0 {
		t.Error("an empty fleet must tell the operator to collect a node")
	}
}

// A second upstream member whose Snapshot is incomplete is a branch, not the
// answer. One unreadable node in the fleet used to mark every Trace that touched
// it partial, including traces whose own path was read completely.
const twoMemberEdge = `
events {}
http {
  upstream app {
    server web02:8080;
    server web05:8080;
  }
  server {
    listen 443 ssl;
    server_name shop.example.com;
    location / { proxy_pass http://app; }
  }
}
`

const degradedBackend = `
events {}
http {
  include /etc/nginx/not-captured.conf;
  server {
    listen 8080;
    server_name shop.example.com;
    location / { root /var/www; }
  }
}
`

func TestDegradedBranchDoesNotDowngradeTheWholeTrace(t *testing.T) {
	db := testDB(t)
	seed(t, db, "lb01", "10.90.4.2", "nginx", "/etc/nginx/nginx.conf", twoMemberEdge)
	seed(t, db, "web02", "10.90.4.3", "nginx", "/etc/nginx/nginx.conf", backend)
	seed(t, db, "web05", "10.90.4.4", "nginx", "/etc/nginx/nginx.conf", degradedBackend)

	tr := Walk(load(t, db), Query{Scheme: "https", Hostname: "shop.example.com", Path: "/pay"})

	var degraded *Hop
	for _, h := range tr.Hops {
		if h.Inst != nil && h.Inst.NodeName == "web05" {
			degraded = h
		}
	}
	if degraded == nil {
		t.Fatal("web05 was never walked, so the fixture is not testing anything")
	}
	if !degraded.Inst.Degraded {
		t.Fatal("web05's snapshot is not degraded, so the fixture is not testing anything")
	}
	if degraded.Confidence != "partial" {
		t.Errorf("the degraded hop = %q, want partial", degraded.Confidence)
	}
	if tr.Confidence == "partial" {
		t.Error("a degraded branch downgraded the whole trace; the followed path was read completely")
	}
	if degraded.Incomplete == "" {
		t.Error("the degraded hop must carry its own reason")
	}
	// The banner is for the followed path: a branch's caveat goes on its Hop.
	for _, n := range tr.Notes {
		if strings.Contains(n, "incomplete") {
			t.Errorf("a branch's incomplete snapshot reached the page banner: %q", n)
		}
	}
}

// `root` is not a proxy target. Reading a path out of it turned the effective
// path into /var/www/static/x — a filesystem path presented as the request's URL
// — and produced a Change with no Rule behind it.
func TestRootIsNotAPathRewrite(t *testing.T) {
	db := testDB(t)
	seed(t, db, "lb01", "10.90.4.2", "nginx", "/etc/nginx/nginx.conf", edge)

	tr := Walk(load(t, db), Query{Scheme: "https", Hostname: "shop.example.com", Path: "/static/x"})
	h := tr.Hops[0]
	if h.EffectivePath != "/static/x" {
		t.Errorf("effective path = %q, want /static/x unchanged", h.EffectivePath)
	}
	for _, c := range h.PathChangedBy {
		t.Errorf("root reported a path change: %+v (rule %v)", c, c.Rule)
	}
	if h.Terminal != TermStatic {
		t.Errorf("terminal = %q, want static_content", h.Terminal)
	}
}
