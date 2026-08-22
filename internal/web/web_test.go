package web

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nagiflow/nagipath/internal/keys"
	"github.com/nagiflow/nagipath/internal/parse"
	"github.com/nagiflow/nagipath/internal/store"
	"github.com/nagiflow/nagipath/internal/trace"
)

// Template errors are runtime errors in Go, so every page is fetched here. A typo
// in a field name is a broken page in production and a failing test now.
func newTestServer(t *testing.T) (*Server, *store.DB) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "web.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	keyPath := filepath.Join(dir, "master.key")
	if err := keys.Generate(keyPath); err != nil {
		t.Fatal(err)
	}
	master, err := keys.Load("", keyPath)
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(db, master, slog.New(slog.NewTextHandler(io.Discard, nil)), false, false)
	if err != nil {
		t.Fatal(err)
	}
	return s, db
}

// client keeps the session cookie and CSRF token across requests, the way a
// browser does.
type client struct {
	t      *testing.T
	s      *Server
	cookie string
	csrf   string
}

func (c *client) get(path string) *httptest.ResponseRecorder {
	c.t.Helper()
	r := httptest.NewRequest("GET", path, nil)
	if c.cookie != "" {
		r.AddCookie(&http.Cookie{Name: cookieName, Value: c.cookie})
	}
	w := httptest.NewRecorder()
	c.s.ServeHTTP(w, r)
	c.absorb(w)
	return w
}

func (c *client) post(path string, form url.Values) *httptest.ResponseRecorder {
	c.t.Helper()
	if c.csrf != "" {
		form.Set("csrf", c.csrf)
	}
	r := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if c.cookie != "" {
		r.AddCookie(&http.Cookie{Name: cookieName, Value: c.cookie})
	}
	w := httptest.NewRecorder()
	c.s.ServeHTTP(w, r)
	c.absorb(w)
	return w
}

func (c *client) absorb(w *httptest.ResponseRecorder) {
	for _, ck := range w.Result().Cookies() {
		if ck.Name == cookieName && ck.Value != "" {
			c.cookie = ck.Value
			c.csrf = csrfToken(ck.Value)
		}
	}
}

func TestFirstRunThenEveryPageRenders(t *testing.T) {
	s, db := newTestServer(t)
	c := &client{t: t, s: s}

	// With no users, everything funnels to setup.
	if got := c.get("/").Code; got != http.StatusSeeOther {
		t.Fatalf("GET / with no users = %d, want a redirect", got)
	}
	if body := c.get("/setup").Body.String(); !strings.Contains(body, "first administrator") {
		t.Fatalf("setup page did not render: %q", body[:min(200, len(body))])
	}

	// A short password is refused rather than accepted quietly.
	w := c.post("/setup", url.Values{"username": {"admin"}, "password": {"short"}, "confirm": {"short"}})
	if loc := w.Header().Get("Location"); !strings.Contains(loc, "err=") {
		t.Errorf("a 5-character password was accepted (redirect to %q)", loc)
	}
	if n, _ := db.UserCount(t.Context()); n != 0 {
		t.Fatal("a user was created despite the rejected password")
	}

	w = c.post("/setup", url.Values{
		"username": {"admin"}, "password": {"a good long password"},
		"confirm": {"a good long password"}})
	if w.Code != http.StatusSeeOther || c.cookie == "" {
		t.Fatalf("setup = %d, cookie %q", w.Code, c.cookie)
	}

	// Setup must close permanently once an admin exists.
	if got := c.post("/setup", url.Values{"username": {"second"}, "password": {"another long password"},
		"confirm": {"another long password"}}).Code; got != http.StatusForbidden {
		t.Errorf("second setup = %d, want 403", got)
	}

	for _, path := range []string{"/", "/nodes", "/instances", "/fleet", "/collections",
		"/certificates", "/certificates?cert=1", "/credentials", "/audit", "/search",
		"/search?q=proxy_pass", "/search?q=proxy_pass&vendor=nginx&page=2", "/trace",
		"/rules", "/rules?hostname=shop.example.com&path=/api", "/drift", "/onboarding",
		"/password"} {
		w := c.get(path)
		if w.Code != http.StatusOK {
			t.Errorf("GET %s = %d\n%s", path, w.Code, w.Body.String())
			continue
		}
		if !strings.Contains(w.Body.String(), "</html>") {
			t.Errorf("GET %s rendered a truncated page (template error mid-render)", path)
		}
	}
}

func TestUnauthenticatedRequestsAreRedirected(t *testing.T) {
	s, db := newTestServer(t)
	if _, err := db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false); err != nil {
		t.Fatal(err)
	}
	c := &client{t: t, s: s}
	for _, path := range []string{"/", "/nodes", "/instances", "/trace", "/audit"} {
		w := c.get(path)
		if w.Code != http.StatusSeeOther || !strings.HasPrefix(w.Header().Get("Location"), "/login") {
			t.Errorf("GET %s while signed out = %d -> %q", path, w.Code, w.Header().Get("Location"))
		}
	}
}

func TestPostWithoutCSRFTokenIsRejected(t *testing.T) {
	s, db := newTestServer(t)
	if _, err := db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false); err != nil {
		t.Fatal(err)
	}
	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})
	if c.cookie == "" {
		t.Fatal("login did not set a session cookie")
	}

	saved := c.csrf
	c.csrf = ""
	if got := c.post("/nodes", url.Values{"address": {"10.0.0.1"}}).Code; got != http.StatusForbidden {
		t.Errorf("POST without a CSRF token = %d, want 403", got)
	}
	c.csrf = "forged-token"
	if got := c.post("/nodes", url.Values{"address": {"10.0.0.1"}}).Code; got != http.StatusForbidden {
		t.Errorf("POST with a forged CSRF token = %d, want 403", got)
	}
	c.csrf = saved
	if got := c.post("/nodes", url.Values{"address": {"10.0.0.1"}}).Code; got != http.StatusSeeOther {
		t.Errorf("POST with the right CSRF token = %d, want 303", got)
	}
	nodes, _ := db.Nodes(t.Context())
	if len(nodes) != 1 {
		t.Errorf("nodes = %d, want 1", len(nodes))
	}
}

// /logout is registered outside auth() (a signed-out request must still reach
// it harmlessly), so it has to check CSRF itself rather than inherit it — this
// pins down that it actually does, and that a forced-password-change user can
// still reach it (auth() would have redirected them to /password instead).
func TestLogoutRequiresCSRFAndWorksMidForcedPasswordChange(t *testing.T) {
	s, db := newTestServer(t)
	if _, err := db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", true); err != nil {
		t.Fatal(err)
	}
	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})
	if c.cookie == "" {
		t.Fatal("login did not set a session cookie")
	}

	saved := c.csrf
	c.csrf = ""
	if got := c.post("/logout", url.Values{}).Code; got != http.StatusForbidden {
		t.Errorf("POST /logout without a CSRF token = %d, want 403", got)
	}
	c.csrf = "forged-token"
	if got := c.post("/logout", url.Values{}).Code; got != http.StatusForbidden {
		t.Errorf("POST /logout with a forged CSRF token = %d, want 403", got)
	}
	sessionBeforeLogout := c.cookie
	c.csrf = saved
	w := c.post("/logout", url.Values{})
	if w.Code != http.StatusSeeOther {
		t.Errorf("POST /logout with the right CSRF token = %d, want 303", w.Code)
	}
	cleared := false
	for _, ck := range w.Result().Cookies() {
		if ck.Name == cookieName && ck.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Error("logout did not send a cookie-clearing Set-Cookie")
	}
	if _, err := db.SessionUser(t.Context(), sessionBeforeLogout); err == nil {
		t.Error("session should be invalidated server-side after logout")
	}
}

// nagipath never scans. A CIDR in the address field is a scan request and must be
// refused, not helpfully expanded.
func TestAddNodeRejectsNetworkRanges(t *testing.T) {
	s, db := newTestServer(t)
	db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false)
	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	w := c.post("/nodes", url.Values{"address": {"10.90.4.0/24"}})
	if loc := w.Header().Get("Location"); !strings.Contains(loc, "scan") {
		t.Errorf("a CIDR was accepted as a node address (redirect %q)", loc)
	}
	if nodes, _ := db.Nodes(t.Context()); len(nodes) != 0 {
		t.Errorf("nodes = %d, want 0", len(nodes))
	}
}

func TestViewerCannotChangeAnything(t *testing.T) {
	s, db := newTestServer(t)
	db.CreateUser(t.Context(), "viewer", "a good long password", "viewer", "Viewer", false)
	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"viewer"}, "password": {"a good long password"}})
	if c.cookie == "" {
		t.Fatal("viewer could not sign in")
	}
	if got := c.get("/nodes").Code; got != http.StatusOK {
		t.Errorf("a viewer must still be able to read /nodes, got %d", got)
	}
	if got := c.post("/nodes", url.Values{"address": {"10.0.0.9"}}).Code; got != http.StatusForbidden {
		t.Errorf("viewer adding a node = %d, want 403", got)
	}
	if got := c.get("/audit").Code; got != http.StatusForbidden {
		t.Errorf("viewer reading the audit log = %d, want 403", got)
	}
}

func TestMustChangePasswordBlocksEverythingElse(t *testing.T) {
	s, db := newTestServer(t)
	db.CreateUser(t.Context(), "admin", "a temporary password", "admin", "Admin", true)
	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a temporary password"}})

	w := c.get("/nodes")
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/password" {
		t.Fatalf("GET /nodes = %d -> %q, want a redirect to /password", w.Code, w.Header().Get("Location"))
	}
	if got := c.get("/password").Code; got != http.StatusOK {
		t.Fatalf("GET /password = %d", got)
	}
	// No current password is asked for, because the temporary one was assigned.
	if got := c.post("/password", url.Values{
		"new": {"a properly chosen password"}, "confirm": {"a properly chosen password"},
	}).Code; got != http.StatusSeeOther {
		t.Fatal("password change was refused")
	}
	if got := c.get("/nodes").Code; got != http.StatusOK {
		t.Errorf("GET /nodes after the change = %d, want 200", got)
	}
}

// A private key must never come back out of the product, by any route.
func TestPrivateKeyIsNeverRendered(t *testing.T) {
	s, db := newTestServer(t)
	db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false)
	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	w := c.post("/credentials", url.Values{
		"name": {"lab"}, "username": {"nagipath"}, "private_key": {testKey},
	})
	if loc := w.Header().Get("Location"); strings.Contains(loc, "err=") {
		t.Fatalf("storing the credential failed: %s", loc)
	}
	creds, _ := db.Credentials(t.Context())
	if len(creds) != 1 {
		t.Fatalf("credentials = %d, want 1", len(creds))
	}
	body := c.get("/credentials").Body.String()
	if !strings.Contains(body, "lab") || !strings.Contains(body, creds[0].Fingerprint) {
		t.Error("the credential is not listed at all")
	}
	// The blank form's placeholder legitimately contains the PEM header, so this
	// looks for the key body itself.
	for _, line := range strings.Fields(testKey) {
		if len(line) > 40 && strings.Contains(body, line) {
			t.Fatalf("the credentials page rendered private key material: %s", line)
		}
	}
}

func TestTraceFormWithNoInstancesSaysSo(t *testing.T) {
	s, db := newTestServer(t)
	db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false)
	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	body := c.get("/trace").Body.String()
	if !strings.Contains(body, "Nothing is collected yet") {
		t.Error("the trace page should say why it cannot trace anything")
	}
	// A trace against an empty fleet must render a result, not a 500.
	w := c.post("/trace", url.Values{"scheme": {"https"}, "hostname": {"shop.example.com"}, "path": {"/"}})
	if w.Code != http.StatusOK {
		t.Fatalf("POST /trace on an empty fleet = %d\n%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "nothing is listening there") {
		t.Error("the result did not explain why the trace stopped")
	}
	// The form posts one pasted URL; the old parameters above still have to work,
	// because every deep link in the app is built from them.
	w = c.post("/trace", url.Values{"url": {"shop.example.com/v2/charge"}})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "https://shop.example.com/v2/charge") {
		t.Errorf("a pasted URL did not become the query = %d\n%s", w.Code, w.Body.String())
	}
}

func TestPastedURLBecomesAQuery(t *testing.T) {
	for _, c := range []struct {
		in   string
		want string
	}{
		// A bare host is https on the default port with the root path, and the
		// default port is not printed back — :443 on every trace is noise.
		{"shop.example.com", "https://shop.example.com/"},
		{"shop.example.com/v2/charge", "https://shop.example.com/v2/charge"},
		{"http://shop.example.com:8080/x", "http://shop.example.com:8080/x"},
		{"https://shop.example.com:8443/", "https://shop.example.com:8443/"},
		// A query string is not part of a path the walk can match on.
		{"  https://SHOP.example.com/a?b=1  ", "https://shop.example.com/a"},
		{"", ""},
	} {
		q, err := parseTarget(c.in)
		if err != nil {
			t.Errorf("parseTarget(%q): %v", c.in, err)
			continue
		}
		got := ""
		if q.Hostname != "" {
			got = targetURL(q.Normalise())
		}
		if got != c.want {
			t.Errorf("parseTarget(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if _, err := parseTarget("http://[::1"); err == nil {
		t.Error("an unparseable paste should be an error, not an empty query")
	}
}

// The trace page shows the rules that decided the hop and nothing else, grouped
// by the file they came from. Global scope is the noise this exists to drop.
// Two members of one upstream are alternatives, not consecutive hops, so they
// have to land on the same rank of the graph.
func TestUpstreamMembersShareOneGraphRank(t *testing.T) {
	levels := hopLevels([]*trace.Hop{
		{Ordinal: 0, ArrivedFrom: -1},
		{Ordinal: 1, ArrivedFrom: 0},
		{Ordinal: 2, ArrivedFrom: 0},
		{Ordinal: 3, ArrivedFrom: 1},
	})
	if len(levels) != 3 || len(levels[0].Hops) != 1 || len(levels[1].Hops) != 2 ||
		len(levels[2].Hops) != 1 {
		t.Fatalf("ranks = %d, want 3 of sizes 1, 2, 1", len(levels))
	}
	if levels[1].Hops[1].Ordinal != 2 {
		t.Errorf("hop 2 is not beside hop 1")
	}
}

func TestHopRulesAreGroupedByFileWithoutGlobals(t *testing.T) {
	rules := []trace.HopRule{
		{Rule: &trace.Rule{ID: 1, Path: "/etc/nginx/nginx.conf"}, Scope: "global", Inherited: true},
		{Rule: &trace.Rule{ID: 2, Path: "/etc/nginx/sites-enabled/shop"}, Scope: "site", Inherited: true},
		{Rule: &trace.Rule{ID: 3, Path: "/etc/nginx/snippets/proxy.conf"}, Scope: "route"},
		{Rule: &trace.Rule{ID: 4, Path: "/etc/nginx/sites-enabled/shop"}, Scope: "route"},
	}
	groups := routingRules(rules)
	if len(groups) != 2 || groups[0].Path != "/etc/nginx/sites-enabled/shop" ||
		len(groups[0].Rules) != 2 || len(groups[1].Rules) != 1 {
		t.Errorf("routingRules grouped wrongly: %+v", groups)
	}
	if n := len(globalRules(rules)); n != 1 {
		t.Errorf("globals = %d, want 1", n)
	}

	// Two branches ending the same way is one sentence, not two.
	same := "the request leaves the fleet"
	hops := []*trace.Hop{
		{Terminal: "external_hop", ExternalReason: same},
		{Terminal: "external_hop", ExternalReason: same},
		{Terminal: "static_content", ExternalReason: "served from disk"},
		{},
	}
	if got := endingReasons(hops); len(got) != 2 || got[0] != same {
		t.Errorf("endingReasons = %q, want the two distinct ones", got)
	}

	next := trace.Next{Member: &trace.Member{Host: "10.90.4.20"}}
	if got := nextURL(next, "/v2/charge"); got != "http://10.90.4.20:80/v2/charge" {
		t.Errorf("nextURL = %q", got)
	}
}

// A certificate that does not cover the name it serves is the failure this
// arithmetic exists to catch, so the wildcard rule has to be exactly RFC 6125's:
// one label, and never the bare domain.
func TestUncoveredNames(t *testing.T) {
	cases := []struct {
		cn      string
		sans    []string
		serves  []string
		want    string
		comment string
	}{
		{"shop.example.com", []string{"shop.example.com", "www.example.com"},
			[]string{"shop.example.com"}, "", "the ordinary case"},
		{"other.example.com", []string{"other.example.com"},
			[]string{"admin.example.com"}, "admin.example.com", "the lab's web05 certificate"},
		{"", []string{"*.example.com"}, []string{"a.example.com"}, "", "a wildcard covers one label"},
		{"", []string{"*.example.com"}, []string{"example.com"},
			"example.com", "a wildcard does not cover the bare domain"},
		{"", []string{"*.example.com"}, []string{"a.b.example.com"},
			"a.b.example.com", "a wildcard does not cover two labels"},
		{"", []string{"Shop.Example.COM"}, []string{"shop.example.com."}, "", "case and trailing dot"},
		{"", nil, []string{"_"}, "", "a catch-all name is not a claim about identity"},
	}
	for _, c := range cases {
		if got := strings.Join(uncovered(c.cn, c.sans, c.serves), ","); got != c.want {
			t.Errorf("%s: uncovered(%q, %v, %v) = %q, want %q",
				c.comment, c.cn, c.sans, c.serves, got, c.want)
		}
	}
}

// A throwaway ed25519 key, generated for this test only.
const testKey = `-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtzc2gtZW
QyNTUxOQAAACBKjOmntBSro1ndD+eIwP42a+NMIblJQSqvNpY9tPR4AgAAAJi1g/r/tYP6
/wAAAAtzc2gtZWQyNTUxOQAAACBKjOmntBSro1ndD+eIwP42a+NMIblJQSqvNpY9tPR4Ag
AAAEB2ROXtat6XEeEl08vk8V8C4iFKTFkxxurJfZOucR9ZTkqM6ae0FKujWd0P54jA/jZr
40whuUlBKq82lj209HgCAAAAEW5hZ2lwYXRoIHRlc3Qga2V5AQIDBA==
-----END OPENSSH PRIVATE KEY-----
`

// The detail pages are the ones with the most template field references and the
// only ones the smoke walk above cannot reach without data, so they get their own
// pass over an empty node and an instance with nothing collected yet.
func TestDetailPagesRender(t *testing.T) {
	s, db := newTestServer(t)
	db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false)
	nodeID, err := db.AddNode(t.Context(), "10.90.4.2", 22, "lb01", "nagipath", nil, nil, "manual", nil)
	if err != nil {
		t.Fatal(err)
	}
	instID, err := db.UpsertInstance(t.Context(), store.Instance{
		NodeID: nodeID, Vendor: "nginx", NaturalKey: "/etc/nginx/nginx.conf",
		DisplayName: "lb01 nginx", MainConfigPath: "/etc/nginx/nginx.conf",
	})
	if err != nil {
		t.Fatal(err)
	}

	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})
	for _, path := range []string{
		"/nodes/" + strconv.FormatInt(nodeID, 10),
		"/instances/" + strconv.FormatInt(instID, 10),
	} {
		w := c.get(path)
		if w.Code != http.StatusOK {
			t.Errorf("GET %s = %d\n%s", path, w.Code, w.Body.String())
			continue
		}
		if !strings.Contains(w.Body.String(), "</html>") {
			t.Errorf("GET %s rendered a truncated page (template error mid-render)", path)
		}
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

// The pages that read a parsed Snapshot reference far more fields than the empty
// ones do, and every one of those references is a runtime error if it is wrong.
// This walks them over a real parse rather than a stub, so a rename in the
// topology or a typo in a template fails here.
func TestPagesRenderOverAParsedSnapshot(t *testing.T) {
	s, db := newTestServer(t)
	db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false)
	ctx := t.Context()

	nodeID, err := db.AddNode(ctx, "10.90.4.2", 22, "lb01", "nagipath", nil, nil, "manual", nil)
	if err != nil {
		t.Fatal(err)
	}
	instID, err := db.UpsertInstance(ctx, store.Instance{
		NodeID: nodeID, Vendor: "nginx", NaturalKey: "/etc/nginx/nginx.conf",
		DisplayName: "lb01 nginx", Version: "nginx/1.24.0",
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

	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})
	for _, path := range []string{
		"/", "/fleet", "/instances", "/instances/" + strconv.FormatInt(instID, 10),
		"/nodes/" + strconv.FormatInt(nodeID, 10),
		"/search?q=proxy_pass", "/rules?hostname=shop.example.com&path=/api/v2",
		"/trace?scheme=https&hostname=shop.example.com&path=/api/v2&port=443",
		// The static route: `root` ends the walk, and the hop panel renders it.
		"/trace?scheme=https&hostname=shop.example.com&path=/&port=443",
		"/drift", "/onboarding", "/certificates",
	} {
		w := c.get(path)
		if w.Code != http.StatusOK {
			t.Errorf("GET %s = %d\n%s", path, w.Code, w.Body.String())
			continue
		}
		if !strings.Contains(w.Body.String(), "</html>") {
			t.Errorf("GET %s rendered a truncated page (template error mid-render)", path)
		}
	}

	// The instance page has to carry what it parsed, not just render.
	body := c.get("/instances/" + strconv.FormatInt(instID, 10)).Body.String()
	for _, want := range []string{"shop.example.com", "upstreams", "web02", "jump to file"} {
		if !strings.Contains(body, want) {
			t.Errorf("instance page is missing %q", want)
		}
	}
}

// seedNginx is one node running one parsed nginx instance, which is the smallest
// fixture the cluster comparison can be run against.
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

// Identical configuration on two nodes is a cluster, discovered without anyone
// asking, named so it can be renamed, and dissolved when the configuration
// diverges. That whole life cycle is what the fleet shows.
func TestClustersAreDiscoveredFromIdenticalConfiguration(t *testing.T) {
	s, db := newTestServer(t)
	ctx := t.Context()
	if _, err := db.CreateUser(ctx, "admin", "a good long password", "admin", "Admin", false); err != nil {
		t.Fatal(err)
	}
	seedNginx(t, db, "web02", "10.90.4.11")
	seedNginx(t, db, "web05", "10.90.4.12")
	// A third host on a different vendor must not be swept in.
	hapNode, err := db.AddNode(ctx, "10.90.4.13", 22, "lb09", "nagipath", nil, nil, "manual", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.UpsertInstance(ctx, store.Instance{
		NodeID: hapNode, Vendor: "haproxy", NaturalKey: "/etc/haproxy/haproxy.cfg",
		DisplayName: "lb09 haproxy", MainConfigPath: "/etc/haproxy/haproxy.cfg",
	}); err != nil {
		t.Fatal(err)
	}

	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})
	body := c.get("/fleet").Body.String()
	if !strings.Contains(body, "instances with identical configuration") {
		t.Errorf("the fleet does not show a discovered cluster:\n%s", body)
	}

	clusters, err := db.Clusters(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(clusters) != 1 || clusters[0].Members != 2 {
		t.Fatalf("clusters = %+v, want one cluster of two", clusters)
	}

	// The name is the operator's, and it survives rediscovery.
	if err := db.RenameCluster(ctx, clusters[0].ID, "edge-eu", nil); err != nil {
		t.Fatal(err)
	}
	if body := c.get("/fleet").Body.String(); !strings.Contains(body, "edge-eu") {
		t.Error("the rename did not survive cluster discovery")
	}

	// Diverge one member: the cluster it was in no longer describes it.
	if _, err := db.W.ExecContext(ctx,
		`UPDATE rule SET raw_text = 'server_tokens on;' WHERE id = (SELECT MIN(id) FROM rule)`); err != nil {
		t.Fatal(err)
	}
	if err := db.ReconcileClusters(ctx); err != nil {
		t.Fatal(err)
	}
	list, err := db.Instances(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range list {
		if in.ClusterID.Valid {
			t.Errorf("%s is still clustered after its configuration diverged", in.DisplayName)
		}
	}
}
