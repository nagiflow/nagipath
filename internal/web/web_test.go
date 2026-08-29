package web

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
	"github.com/nagiflow/nagipath/internal/keys"
	"github.com/nagiflow/nagipath/internal/parse"
	"github.com/nagiflow/nagipath/internal/store"
	"google.golang.org/protobuf/encoding/protojson"
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
	// Server logs are discarded so a passing run is quiet. A render error or a
	// swallowed store error is reported through the logger and nowhere else, so
	// NAGIPATH_TEST_LOG=1 turns them back on when a test fails for a reason the
	// assertion cannot name.
	var logw io.Writer = io.Discard
	if os.Getenv("NAGIPATH_TEST_LOG") != "" {
		logw = os.Stderr
	}
	s, err := New(db, master, slog.New(slog.NewTextHandler(logw, nil)), false, false, nil, "",
		filepath.Join(dir, "license.lic"))
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

// postJSON is api/client.ts's real request shape for the SPA's internal/api
// endpoints: a JSON body and the CSRF token as a header, not a form field —
// internal/api/middleware.go's checkCSRF only ever looks at the header.
func (c *client) postJSON(path string, body any) *httptest.ResponseRecorder {
	c.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			c.t.Fatalf("encode request body: %v", err)
		}
	}
	r := httptest.NewRequest("POST", path, &buf)
	r.Header.Set("Content-Type", "application/json")
	if c.csrf != "" {
		r.Header.Set("X-CSRF-Token", c.csrf)
	}
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

	// Every screen, on an empty fleet. A template referring to a field its data
	// does not have only fails when that branch is executed, so the branch has to
	// be executed by something — and an empty fleet is the state every install
	// starts in.
	// /instances removed: it redirects to /nodes, which is already tested.
	// "/", "/clusters", "/sites", "/nodes", "/collections", "/rules", "/search",
	// "/trace", "/trace/history" and every "/settings/*" page removed: they all
	// serve the React SPA shell now (TestDashboardServesSPAShell,
	// TestClustersServesSPAShell, TestSitesServesSPAShell, TestNodesServesSPAShell,
	// TestAnalysisServesSPAShell, TestSettingsServesSPAShell,
	// TestExploreServesSPAShell), not a server-rendered page with the
	// class="pnl"/class="empty" chrome below.
	for _, path := range []string{
		"/nodes/import",
		"/password"} {
		w := c.get(path)
		if w.Code != http.StatusOK {
			t.Errorf("GET %s = %d\n%s", path, w.Code, w.Body.String())
			continue
		}
		if !strings.Contains(w.Body.String(), "</html>") {
			t.Errorf("GET %s rendered a truncated page (template error mid-render)", path)
		}
		// A complete page with nothing on it. {{with .Data}} around a whole screen
		// skips its own {{else}} when Data is an empty slice, so four settings pages
		// rendered a chrome-only 200 on an empty fleet — Host keys showed no list, no
		// empty state and, on Credentials and Users, not even the form that would
		// have created the first one.
		if body := w.Body.String(); !strings.Contains(body, `class="pnl`) &&
			!strings.Contains(body, `class="empty"`) {
			t.Errorf("GET %s rendered the chrome and no content at all", path)
		}
	}

}

// The Content-Security-Policy sets script-src 'self' with no 'unsafe-inline', so
// an inline handler or an inline <script> in a template does not misbehave — it
// does not run at all. That failure is silent: the markup looks right, the button
// renders, and clicking it does nothing. Three shipped controls were dead this way
// (a confirm() on an irreversible node delete, one on disabling a user, and three
// filter selects that submitted nothing), which is why this is a test and not a
// review note.
func TestNoTemplateReliesOnInlineScript(t *testing.T) {
	files, err := fs.Glob(assets, "templates/*.html")
	if err != nil {
		t.Fatal(err)
	}
	parts, err := fs.Glob(assets, "templates/parts/*.html")
	if err != nil {
		t.Fatal(err)
	}
	// Every HTML event attribute reachable from a template we actually write.
	attrs := []string{"onclick=", "onsubmit=", "onchange=", "onload=", "oninput=",
		"onkeydown=", "onkeyup=", "onfocus=", "onblur=", "onmouseover=", "onerror="}
	for _, name := range append(files, parts...) {
		b, err := assets.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		// Comments explain the ban; they are not markup.
		body := regexp.MustCompile(`(?s)\{\{/\*.*?\*/\}\}`).ReplaceAllString(string(b), "")
		for _, a := range attrs {
			if strings.Contains(body, a) {
				t.Errorf("%s uses %s — the CSP blocks it, so that control is dead. "+
					"Use <details>, a real form, htmx, or a delegated listener in app.js.", name, a)
			}
		}
		// <script src="..."> is fine; a <script> with a body is not.
		for _, m := range regexp.MustCompile(`(?s)<script[^>]*>(.*?)</script>`).FindAllStringSubmatch(body, -1) {
			if strings.TrimSpace(m[1]) != "" {
				t.Errorf("%s has an inline <script> body, which the CSP blocks", name)
			}
		}
	}
}

// `.wrap` is the app-shell flex container (flex:1;min-height:0;display:flex). Seven
// table cells carried it as if it meant "let this cell wrap", so each of those cells
// was a flex container: a subject plus a dimmed note rendered side by side rather
// than stacked, and on the Dashboard "app01.internal.example.com" ran straight into
// "expires today". The cell modifier is `.brk`.
func TestNoTableCellCarriesTheShellWrapClass(t *testing.T) {
	files, err := fs.Glob(assets, "templates/*.html")
	if err != nil {
		t.Fatal(err)
	}
	cell := regexp.MustCompile(`<t[dh][^>]*class="[^"]*\bwrap\b`)
	for _, name := range files {
		b, err := assets.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if m := cell.Find(b); m != nil {
			t.Errorf(`%s: %q — .wrap makes the cell display:flex; use class="brk"`, name, m)
		}
	}
}

// The CSS reset sets `ol,ul,menu{list-style:none}`, so a list written as a list
// renders as unmarked lines. That is a content bug, not a cosmetic one: the Master
// key rotation steps lost their numbers while the paragraph under them said "until
// step 5", and Retention's four separate exemptions read as one paragraph. Either
// declare a marker or declare that you want none.
func TestEveryListDeclaresItsMarker(t *testing.T) {
	files, err := fs.Glob(assets, "templates/*.html")
	if err != nil {
		t.Fatal(err)
	}
	parts, err := fs.Glob(assets, "templates/parts/*.html")
	if err != nil {
		t.Fatal(err)
	}
	lists := regexp.MustCompile(`<(ol|ul)\b[^>]*>`)
	for _, name := range append(files, parts...) {
		b, err := assets.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, tag := range lists.FindAllString(string(b), -1) {
			if !strings.Contains(tag, "list-style") {
				t.Errorf("%s: %s has no list-style — the reset removes markers, so add "+
					"list-style:disc/decimal, or list-style:none to say the bareness is deliberate", name, tag)
			}
			// display:flex drops markers even when list-style asks for them.
			if strings.Contains(tag, "display:flex") && !strings.Contains(tag, "list-style:none") {
				t.Errorf("%s: %s is a flex container, which has no markers to show", name, tag)
			}
		}
	}
}

// "cols" is a whole header row, so wrapping it in another <tr> emits an empty row
// above the header. Four templates did, and on the Credentials list the phantom row
// was part of why its one real row was drawn outside its panel.
func TestNoTemplateWrapsTheHeaderRowInAnotherRow(t *testing.T) {
	files, err := fs.Glob(assets, "templates/*.html")
	if err != nil {
		t.Fatal(err)
	}
	wrapped := regexp.MustCompile(`<tr>\s*\{\{template "cols"`)
	for _, name := range files {
		b, err := assets.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if wrapped.Match(b) {
			t.Errorf(`%s wraps {{template "cols"}} in a <tr> — cols is the row`, name)
		}
	}
}

// The two screens shown before anyone has signed in. They get a bare <main> from
// layout.html rather than the app shell, so they are the easiest pages in the
// product to leave styled by nothing at all — which is what happened: both were
// written against .auth-card/.narrow/.hint, none of which exist.
func TestSignedOutPagesUseRealClasses(t *testing.T) {
	css, err := assets.ReadFile("static/app.css")
	if err != nil {
		t.Fatal(err)
	}
	class := regexp.MustCompile(`class="([^"{}]*)"`)
	for _, name := range []string{"templates/login.html", "templates/setup.html"} {
		b, err := assets.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range class.FindAllStringSubmatch(string(b), -1) {
			for _, c := range strings.Fields(m[1]) {
				if !bytes.Contains(css, []byte("."+c)) {
					t.Errorf("%s uses class %q, which app.css does not define", name, c)
				}
			}
		}
	}
}

func TestUnauthenticatedRequestsAreRedirected(t *testing.T) {
	s, db := newTestServer(t)
	if _, err := db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false); err != nil {
		t.Fatal(err)
	}
	c := &client{t: t, s: s}
	// /instances removed: it redirects to /nodes, which is already tested.
	for _, path := range []string{"/", "/nodes", "/trace", "/settings/audit"} {
		w := c.get(path)
		if w.Code != http.StatusSeeOther || !strings.HasPrefix(w.Header().Get("Location"), "/login") {
			t.Errorf("GET %s while signed out = %d -> %q", path, w.Code, w.Header().Get("Location"))
		}
	}
}

// A 404 has to be a page, not a bare line of text: the operator gets here by
// mistyping a URL or by following a link to a node someone removed, and both
// cases need the sidebar to get back out.
func TestNotFoundIsAPageInsideTheShell(t *testing.T) {
	s, db := newTestServer(t)
	if _, err := db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false); err != nil {
		t.Fatal(err)
	}
	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	// A mistyped URL, and an id that does not exist: different routes, same page.
	// /instances/4242 removed: it redirects (301) rather than 404ing. /nodes/4242
	// and /settings/{anything} removed: both are the SPA now (docs/adr/0017) and
	// always 200 at the Go level — a bad id or section is a 404 from the JSON API
	// instead, which the page renders as its own not-found state client-side.
	// Same for /snapshots/4242/file/1 — that route is the SPA now too.
	for _, path := range []string{"/does-not-exist"} {
		w := c.get(path)
		if w.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, w.Code)
		}
		body := w.Body.String()
		if !strings.Contains(body, "</html>") || !strings.Contains(body, "Dashboard") {
			t.Errorf("GET %s did not render the app shell:\n%s", path, body)
		}
		if !strings.Contains(body, path) {
			t.Errorf("GET %s did not name the path it could not find", path)
		}
	}

	// Still signed out first: a stranger learns nothing about which paths exist.
	c2 := &client{t: t, s: s}
	if w := c2.get("/no-such-page"); w.Code != http.StatusSeeOther {
		t.Errorf("GET a missing page while signed out = %d, want a redirect to login", w.Code)
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

	// Adding a node is the SPA now (docs/adr/0017): POST /api/ui/nodes
	// (internal/api/nodes.go), CSRF checked via header only (middleware.go).
	saved := c.csrf
	c.csrf = ""
	if got := c.postJSON("/api/ui/nodes", map[string]any{"address": "10.0.0.1"}).Code; got != http.StatusForbidden {
		t.Errorf("POST without a CSRF token = %d, want 403", got)
	}
	c.csrf = "forged-token"
	if got := c.postJSON("/api/ui/nodes", map[string]any{"address": "10.0.0.1"}).Code; got != http.StatusForbidden {
		t.Errorf("POST with a forged CSRF token = %d, want 403", got)
	}
	c.csrf = saved
	if got := c.postJSON("/api/ui/nodes", map[string]any{"address": "10.0.0.1"}).Code; got != http.StatusOK {
		t.Errorf("POST with the right CSRF token = %d, want 200", got)
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

// Direct TLS (docs/infra/customer_deployment.md §5) is opted into with both
// NAGIPATH_TLS_CERT and NAGIPATH_TLS_KEY. One without the other is almost always
// a typo'd env var, not an intentional choice, and must refuse rather than
// silently falling back to plain HTTP with a customer's session cookies on it.
func TestListenRequiresBothTLSFilesOrNeither(t *testing.T) {
	s, _ := newTestServer(t)
	if err := s.Listen(t.Context(), "127.0.0.1:0", "/tmp/only-cert.pem", ""); err == nil {
		t.Error("Listen with a cert but no key should refuse, not drop to plain HTTP")
	}
	if err := s.Listen(t.Context(), "127.0.0.1:0", "", "/tmp/only-key.pem"); err == nil {
		t.Error("Listen with a key but no cert should refuse, not drop to plain HTTP")
	}
}

// nagipath never scans. A CIDR in the address field is a scan request and must be
// refused, not helpfully expanded.
func TestAddNodeRejectsNetworkRanges(t *testing.T) {
	s, db := newTestServer(t)
	db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false)
	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	w := c.postJSON("/api/ui/nodes", map[string]any{"address": "10.90.4.0/24"})
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	json.Unmarshal(w.Body.Bytes(), &body)
	if w.Code != http.StatusUnprocessableEntity || body.Error.Code != "no_scanning" {
		t.Errorf("a CIDR was accepted as a node address (status %d, body %s)", w.Code, w.Body.String())
	}
	if nodes, _ := db.Nodes(t.Context()); len(nodes) != 0 {
		t.Errorf("nodes = %d, want 0", len(nodes))
	}
}

// A second user is the whole point of /users: an admin creates one, and it can
// sign in with the role it was given.
func TestAdminCreatesASecondUserWhoCanSignIn(t *testing.T) {
	s, db := newTestServer(t)
	db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false)
	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	w := c.postJSON("/api/ui/settings/users", map[string]any{
		"username": "newviewer", "password": "a good long password", "confirm": "a good long password", "role": "viewer",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("creating a user failed: %d %s", w.Code, w.Body.String())
	}
	users, err := db.Users(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 2 {
		t.Fatalf("users = %d, want 2", len(users))
	}
	var list pb.UsersResponse
	if err := protojson.Unmarshal(c.get("/api/ui/settings/users").Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, u := range list.Users {
		if u.Username == "newviewer" && u.Role == "viewer" {
			found = true
		}
	}
	if !found {
		t.Error("the new user is not listed")
	}

	// The new account signs in with the role it was created with.
	c2 := &client{t: t, s: s}
	w = c2.post("/login", url.Values{"username": {"newviewer"}, "password": {"a good long password"}})
	if w.Code != http.StatusSeeOther || c2.cookie == "" {
		t.Fatalf("the new user could not sign in: %d", w.Code)
	}
	if got := c2.get("/api/ui/settings/users").Code; got != http.StatusForbidden {
		t.Errorf("a viewer reading /settings/users = %d, want 403", got)
	}
	if got := c2.postJSON("/api/ui/settings/users", map[string]any{
		"username": "x", "password": "a good long password", "confirm": "a good long password", "role": "viewer",
	}).Code; got != http.StatusForbidden {
		t.Errorf("a viewer creating a user = %d, want 403", got)
	}

	// A short password is refused rather than accepted quietly, the same as /setup.
	w = c.postJSON("/api/ui/settings/users", map[string]any{
		"username": "short", "password": "tooshort", "confirm": "tooshort", "role": "viewer",
	})
	if w.Code != http.StatusUnprocessableEntity {
		t.Error("a short password was accepted for a new user")
	}
}

// Disabling a user must actually stop them from signing in, not just hide the
// account from the list — the guardrail that matters is on Authenticate.
func TestDisablingAUserPreventsLogin(t *testing.T) {
	s, db := newTestServer(t)
	db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false)
	viewerID, err := db.CreateUser(t.Context(), "viewer", "a good long password", "viewer", "Viewer", false)
	if err != nil {
		t.Fatal(err)
	}
	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	w := c.postJSON("/api/ui/settings/users/"+strconv.FormatInt(viewerID, 10)+"/disable", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("disabling the viewer failed: %d %s", w.Code, w.Body.String())
	}

	c2 := &client{t: t, s: s}
	w = c2.post("/login", url.Values{"username": {"viewer"}, "password": {"a good long password"}})
	if c2.cookie != "" || !strings.Contains(w.Header().Get("Location"), "err=") {
		t.Fatal("a disabled user was able to sign in")
	}

	// Re-enabling restores it.
	c.postJSON("/api/ui/settings/users/"+strconv.FormatInt(viewerID, 10)+"/enable", nil)
	c3 := &client{t: t, s: s}
	c3.post("/login", url.Values{"username": {"viewer"}, "password": {"a good long password"}})
	if c3.cookie == "" {
		t.Fatal("the re-enabled user could not sign in")
	}
}

// The structural guardrail: disabling the sole admin must be refused, not
// merely discouraged, because there would be nobody left to undo it.
func TestLastAdminCannotBeDisabled(t *testing.T) {
	s, db := newTestServer(t)
	adminID, err := db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false)
	if err != nil {
		t.Fatal(err)
	}
	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	w := c.postJSON("/api/ui/settings/users/"+strconv.FormatInt(adminID, 10)+"/disable", nil)
	if w.Code != http.StatusUnprocessableEntity {
		t.Error("disabling the last admin was allowed")
	}
	u, err := db.User(t.Context(), adminID)
	if err != nil {
		t.Fatal(err)
	}
	if u.Disabled {
		t.Error("the last admin was disabled despite the guardrail")
	}

	// With a second enabled admin, disabling the first is allowed.
	db.CreateUser(t.Context(), "admin2", "a good long password", "admin", "Admin2", false)
	w = c.postJSON("/api/ui/settings/users/"+strconv.FormatInt(adminID, 10)+"/disable", nil)
	if w.Code != http.StatusOK {
		t.Errorf("disabling one of two admins was refused: %d %s", w.Code, w.Body.String())
	}
}

// Failed logins are throttled per username, checked before the (deliberately
// slow) password hash runs, and a limit of 0 turns the guardrail off.
func TestLoginRateLimitBlocksThenClearsAfterTheWindow(t *testing.T) {
	s, db := newTestServer(t)
	db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false)
	if err := db.SetSetting(t.Context(), "login_rate_limit_max", "3", nil); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSetting(t.Context(), "login_rate_limit_window_seconds", "300", nil); err != nil {
		t.Fatal(err)
	}

	c := &client{t: t, s: s}
	for i := 0; i < 3; i++ {
		w := c.post("/login", url.Values{"username": {"admin"}, "password": {"wrong password"}})
		if !strings.Contains(w.Header().Get("Location"), "err=") {
			t.Fatalf("attempt %d: wrong password was not refused", i)
		}
	}
	// The ceiling is reached: even the correct password is refused now, without
	// ever reaching Authenticate.
	w := c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})
	if c.cookie != "" {
		t.Fatal("login succeeded despite the rate limit")
	}
	if !strings.Contains(w.Header().Get("Location"), "err=") {
		t.Fatal("a rate-limited login was not refused")
	}

	// A different username is not caught by the same limit.
	db.CreateUser(t.Context(), "someoneelse", "a good long password", "viewer", "Someone", false)
	c2 := &client{t: t, s: s}
	c2.post("/login", url.Values{"username": {"someoneelse"}, "password": {"a good long password"}})
	if c2.cookie == "" {
		t.Error("a different username was blocked by another account's rate limit")
	}

	// Once the window has passed, the correct password works again.
	if _, err := db.W.ExecContext(t.Context(),
		`UPDATE audit_event SET at = ? WHERE action = 'auth.login' AND target_label = 'admin'`,
		time.Now().UTC().Add(-10*time.Minute).Format("2006-01-02T15:04:05Z")); err != nil {
		t.Fatal(err)
	}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})
	if c.cookie == "" {
		t.Error("login after the rate-limit window passed should have succeeded")
	}

	// A limit of 0 disables the guardrail rather than blocking everything.
	if err := db.SetSetting(t.Context(), "login_rate_limit_max", "0", nil); err != nil {
		t.Fatal(err)
	}
	c3 := &client{t: t, s: s}
	for i := 0; i < 5; i++ {
		c3.post("/login", url.Values{"username": {"admin"}, "password": {"wrong password"}})
	}
	w = c3.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})
	if c3.cookie == "" {
		t.Errorf("login_rate_limit_max=0 should disable the guardrail, got %d -> %q",
			w.Code, w.Header().Get("Location"))
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
	if got := c.postJSON("/api/ui/nodes", map[string]any{"address": "10.0.0.9"}).Code; got != http.StatusForbidden {
		t.Errorf("viewer adding a node = %d, want 403", got)
	}
	if got := c.get("/api/ui/settings/audit").Code; got != http.StatusOK {
		t.Errorf("viewer reading the audit log = %d, want 200", got)
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

	w := c.postJSON("/api/ui/settings/credentials", map[string]any{
		"name": "lab", "username": "nagipath", "privateKey": testKey,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("storing the credential failed: %d %s", w.Code, w.Body.String())
	}
	creds, _ := db.Credentials(t.Context())
	if len(creds) != 1 {
		t.Fatalf("credentials = %d, want 1", len(creds))
	}
	body := c.get("/api/ui/settings/credentials").Body.String()
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

	var empty pb.TraceResponse
	if err := protojson.Unmarshal(c.get("/api/ui/trace").Body.Bytes(), &empty); err != nil {
		t.Fatal(err)
	}
	if !empty.Empty {
		t.Error("the trace response should say why it cannot trace anything")
	}
	// A trace against an empty fleet must render a result, not a 500.
	w := c.get("/api/ui/trace?scheme=https&hostname=shop.example.com&path=/")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/ui/trace on an empty fleet = %d\n%s", w.Code, w.Body.String())
	}
	var result pb.TraceResponse
	if err := protojson.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.TerminalReason != "nothing is listening there" {
		t.Errorf("the result did not explain why the trace stopped: %q", result.TerminalReason)
	}
	// The old scheme/hostname/path parameters still work, because every deep
	// link in the app is built from them; a pasted url=... also works.
	w = c.get("/api/ui/trace?url=" + url.QueryEscape("shop.example.com/v2/charge"))
	result = pb.TraceResponse{}
	if err := protojson.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || result.Url != "https://shop.example.com/v2/charge" {
		t.Errorf("a pasted URL did not become the query = %d, url=%q", w.Code, result.Url)
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
	if _, err := db.UpsertInstance(t.Context(), store.Instance{
		NodeID: nodeID, Vendor: "nginx", NaturalKey: "/etc/nginx/nginx.conf",
		DisplayName: "lb01 nginx", MainConfigPath: "/etc/nginx/nginx.conf",
	}); err != nil {
		t.Fatal(err)
	}

	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})
	// /instances/{id} removed: it redirects to /nodes/{nodeID}?process={id}.
	// The node detail page is already tested, and the redirect is tested separately.
	for _, path := range []string{
		"/nodes/" + strconv.FormatInt(nodeID, 10),
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
	// Two certificates, one expired and one with a year left, because the list only
	// renders rows when there are rows: an expiry comparison that is a template
	// error reached a browser with /certificates already in this loop.
	for i, notAfter := range []string{
		time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339),
		time.Now().Add(400 * 24 * time.Hour).UTC().Format(time.RFC3339),
	} {
		certID, err := db.UpsertCertificate(ctx, store.Certificate{
			Fingerprint: fmt.Sprintf("SHA256:test%d", i),
			SubjectCN:   "shop.example.com", SubjectDN: "CN=shop.example.com",
			SANs: []string{"shop.example.com"}, IssuerDN: "CN=Lab CA",
			NotBefore: time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339),
			NotAfter:  notAfter, KeyAlgorithm: "rsaEncryption",
			KeyBits: sql.NullInt64{Int64: 2048, Valid: true},
		})
		if err != nil {
			t.Fatal(err)
		}
		// Bound, not merely stored: a certificate nothing serves is a row in a table,
		// and every screen that answers "what is this cert doing" reads the binding.
		if err := db.AddCertBinding(ctx, store.CertBindingRow{
			CertificateID: certID, InstanceID: instID, SnapshotID: snapID,
			FilePath: "/etc/ssl/shop.example.com.pem",
		}); err != nil {
			t.Fatal(err)
		}
	}

	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})
	// Node, not instance: the tabs hang off the node with the process in the query
	// string. The old /instances URLs are checked below, as redirects.
	node := "/nodes/" + strconv.FormatInt(nodeID, 10)
	proc := "?process=" + strconv.FormatInt(instID, 10)
	for _, path := range []string{
		"/", "/nodes", "/sites", "/sites/shop.example.com", "/snapshots", node,
		// Every tab, because a tab nothing renders is a tab nothing checks: the
		// fields these templates read are only resolved when their branch runs.
		node + "/sites" + proc, node + "/routes" + proc, node + "/upstreams" + proc,
		node + "/certificates" + proc, node + "/files" + proc, node + "/drift" + proc,
		"/search?q=proxy_pass", "/rules?hostname=shop.example.com&path=/api/v2",
		"/trace?scheme=https&hostname=shop.example.com&path=/api/v2&port=443",
		// The static route: `root` ends the walk, and the hop panel renders it.
		"/trace?scheme=https&hostname=shop.example.com&path=/&port=443",
		"/drift", "/nodes/import", "/certificates", "/certificates?cert=1", "/certificates?cert=2",
		// The every-instance scope, which hides the golden-peer and ignore controls
		// and so takes a different set of branches from the cluster scope.
		"/drift?cluster=all", "/drift?cluster=all&group=instance",
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
	// The instance hierarchy is gone, and every link anyone had bookmarked has to
	// land on the same process under the node that replaced it.
	for path, want := range map[string]string{
		"/instances":                             "/nodes",
		"/instances/" + itoa(instID):             node + proc,
		"/instances/" + itoa(instID) + "/routes": node + "/routes" + proc,
	} {
		w := c.get(path)
		if w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != want {
			t.Errorf("GET %s = %d -> %q, want 301 -> %q", path, w.Code, w.Header().Get("Location"), want)
		}
	}

	// Sites is the SPA now (docs/adr/0017); the CSV export moved with it to
	// GET /api/ui/sites?export=csv (internal/api/sites.go).
	if w := c.get("/api/ui/sites?export=csv"); w.Code != http.StatusOK ||
		!strings.Contains(w.Header().Get("Content-Type"), "text/csv") ||
		!strings.Contains(w.Body.String(), "shop.example.com") {
		t.Errorf("GET /api/ui/sites?export=csv did not return the filtered site export: %d %q", w.Code, w.Body.String())
	}

	// Search is the SPA now (docs/adr/0017, Phase 5); GET /api/ui/search
	// (internal/api/search.go) returns a typed proto response, so the class of
	// bug this used to guard — a template reading a field store.TextHit does
	// not have — cannot recur: a mismatched field fails to compile.
	if w := c.get("/api/ui/search?q=proxy_pass"); w.Code != http.StatusOK {
		t.Errorf("GET /api/ui/search?q=proxy_pass = %d\n%s", w.Code, w.Body.String())
	}

	// A POST stores the Trace, and the bare GET then lists it. That table was
	// only ever rendered against an empty history, so a field it read that
	// trace.Summary does not have reached a real install as a 500.
	if w := c.postJSON("/api/ui/trace", map[string]any{"url": "shop.example.com/api/v2"}); w.Code != http.StatusOK {
		t.Fatalf("POST /api/ui/trace = %d\n%s", w.Code, w.Body.String())
	}
	var recent pb.TraceResponse
	recentW := c.get("/api/ui/trace")
	if recentW.Code != http.StatusOK {
		t.Fatalf("GET /api/ui/trace with a stored trace = %d\n%s", recentW.Code, recentW.Body.String())
	}
	if err := protojson.Unmarshal(recentW.Body.Bytes(), &recent); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range recent.Recent {
		if r.Scheme+"://"+r.Hostname+r.Path == "https://shop.example.com/api/v2" {
			found = true
		}
	}
	if !found {
		t.Errorf("the recent-traces list is missing the stored trace: %+v", recent.Recent)
	}

	// Nodes is the SPA now (docs/adr/0017); what used to be HTML substring checks
	// per tab are field assertions against GET /api/ui/nodes/{id}[/{tab}] instead.
	nodeAPI := "/api/ui/nodes/" + strconv.FormatInt(nodeID, 10)

	var overview pb.NodeDetailResponse
	if err := protojson.Unmarshal(c.get(nodeAPI+proc).Body.Bytes(), &overview); err != nil {
		t.Fatalf("decode node overview: %v", err)
	}
	foundSite := false
	for _, site := range overview.Inst.GetSites() {
		if site.PrimaryName == "shop.example.com" {
			foundSite = true
		}
	}
	if !foundSite {
		t.Errorf("node overview's Inst.Sites is missing shop.example.com: %+v", overview.Inst.GetSites())
	}

	var sitesTab pb.NodeDetailResponse
	if err := protojson.Unmarshal(c.get(nodeAPI+"/sites"+proc).Body.Bytes(), &sitesTab); err != nil {
		t.Fatalf("decode node sites tab: %v", err)
	}
	if len(sitesTab.Inst.GetSites()) == 0 || len(sitesTab.Inst.GetSites()[0].Routes) == 0 {
		t.Errorf("node sites tab carries no routes to provide provenance for: %+v", sitesTab.Inst.GetSites())
	}

	var routesTab pb.NodeDetailResponse
	if err := protojson.Unmarshal(c.get(nodeAPI+"/routes"+proc).Body.Bytes(), &routesTab); err != nil {
		t.Fatalf("decode node routes tab: %v", err)
	}
	if routesTab.SelectedRoute == nil || !strings.Contains(routesTab.SelectedRoute.Pattern, "/api/") {
		t.Errorf("node routes tab's selected route = %+v, want a pattern containing /api/", routesTab.SelectedRoute)
	}

	var upstreamsTab pb.NodeDetailResponse
	if err := protojson.Unmarshal(c.get(nodeAPI+"/upstreams"+proc).Body.Bytes(), &upstreamsTab); err != nil {
		t.Fatalf("decode node upstreams tab: %v", err)
	}
	foundMember := false
	for _, m := range upstreamsTab.PoolMembers {
		if m.Host == "web02" {
			foundMember = true
		}
	}
	if !foundMember {
		t.Errorf("node upstreams tab's pool members = %+v, want web02 among them", upstreamsTab.PoolMembers)
	}

	var certsTab pb.NodeDetailResponse
	if err := protojson.Unmarshal(c.get(nodeAPI+"/certificates"+proc).Body.Bytes(), &certsTab); err != nil {
		t.Fatalf("decode node certificates tab: %v", err)
	}
	foundCert := false
	for _, cb := range certsTab.Certificates {
		if strings.Contains(cb.SubjectCn, "shop.example") {
			foundCert = true
		}
	}
	if !foundCert {
		t.Errorf("node certificates tab = %+v, want a binding naming shop.example", certsTab.Certificates)
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

	// Clusters is the React SPA now (docs/adr/0017); the fact it used to read off
	// rendered HTML is asserted against GET /api/ui/clusters instead, decoded via
	// protojson against the generated schema (docs/adr/0018). One cluster, two
	// members, vendor nginx, the third (different-vendor) host excluded.
	getClusters := func() *pb.ClustersResponse {
		t.Helper()
		got := &pb.ClustersResponse{}
		if err := protojson.Unmarshal(c.get("/api/ui/clusters").Body.Bytes(), got); err != nil {
			t.Fatalf("decode /api/ui/clusters: %v", err)
		}
		return got
	}

	got := getClusters()
	if len(got.Clusters) != 1 || got.Clusters[0].Members != 2 || got.Clusters[0].Vendor != "nginx" {
		t.Fatalf("api/v1/clusters = %+v, want one cluster of two nginx instances", got.Clusters)
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
	if got := getClusters(); len(got.Clusters) != 1 || got.Clusters[0].Name != "edge-eu" {
		t.Errorf("the rename did not survive cluster discovery: %+v", got.Clusters)
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

// redirect used to append "?ok=..." unconditionally, so a redirect back to a
// filtered list produced /drift?cluster=all?ok=... — the second "?" landed inside
// the cluster value, the scope was silently lost, and the message never rendered.
func TestRedirectKeepsExistingQuery(t *testing.T) {
	for _, tc := range []struct{ path, ok, err, want string }{
		{"/drift?cluster=all", "5 compared", "", "/drift?cluster=all&ok=5+compared"},
		{"/drift", "5 compared", "", "/drift?ok=5+compared"},
		{"/collections?range=7d", "", "no such node", "/collections?range=7d&err=no+such+node"},
		{"/nodes", "", "", "/nodes"},
	} {
		w := httptest.NewRecorder()
		redirect(w, httptest.NewRequest("POST", "/x", nil), tc.path, tc.ok, tc.err)
		if got := w.Header().Get("Location"); got != tc.want {
			t.Errorf("redirect(%q) = %q, want %q", tc.path, got, tc.want)
		}
		// The round trip is what matters: the scope has to survive as its own value.
		u, err := url.Parse(w.Header().Get("Location"))
		if err != nil {
			t.Fatalf("redirect produced an unparseable URL: %v", err)
		}
		if strings.Contains(u.RawQuery, "?") {
			t.Errorf("redirect(%q) put a %q inside the query: %q", tc.path, "?", u.RawQuery)
		}
	}
}

// The drift findings table had never been rendered with a finding in it, so the
// provenance link it draws — the product's trust mechanism — referred to a field
// store.DriftFinding does not have, and /drift 500ed the moment anything drifted.
// Go template field errors are lazy: only executing the branch finds them.
func TestDriftRendersItsFindingsAndProvenance(t *testing.T) {
	s, db := newTestServer(t)
	ctx := t.Context()
	if _, err := db.CreateUser(ctx, "admin", "a good long password", "admin", "Admin", false); err != nil {
		t.Fatal(err)
	}
	instID := seedNginx(t, db, "web02", "10.90.4.11")

	// A second capture of the same instance with one upstream member moved. That is
	// the previous-snapshot baseline, the only one a single host can have.
	changed := strings.Replace(testNginx, "server web02:8080;", "server web02:9090;", 1)
	if changed == testNginx {
		t.Fatal("the fixture no longer contains the upstream this test moves")
	}
	colID, err := db.StartCollection(ctx, 1, "manual", nil)
	if err != nil {
		t.Fatal(err)
	}
	files := []parse.File{{Path: "/etc/nginx/nginx.conf", Content: []byte(changed)}}
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

	stored, skipped := db.RecomputeCluster(ctx, 0)
	if stored == 0 {
		t.Fatalf("nothing was compared: %v", skipped)
	}
	findings, err := db.DriftFindings(ctx, []int64{1})
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) == 0 {
		t.Fatal("moving an upstream member produced no drift finding, so this test proves nothing")
	}
	// Provenance is only checkable if the path came back with the finding.
	var withPath int
	for _, f := range findings {
		if f.FileID.Valid && f.Path != "" {
			withPath++
		}
	}
	if withPath == 0 {
		t.Error("no finding carries the file it was parsed from, so its provenance link cannot be built")
	}

	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	// Drift is the React SPA now (docs/adr/0017); the same guarantees are
	// asserted against GET /api/ui/drift and /api/ui/drift/review/{id}
	// (docs/adr/0018) instead of rendered HTML.
	var driftResp pb.DriftResponse
	if err := protojson.Unmarshal(c.get("/api/ui/drift?cluster=all").Body.Bytes(), &driftResp); err != nil {
		t.Fatalf("decode /api/ui/drift?cluster=all: %v", err)
	}
	// /drift is the summary: which nodes are off baseline and by how much. The
	// findings themselves live one click deeper, on the review screen.
	if len(driftResp.InstancesWithDrift) == 0 {
		t.Errorf("/api/ui/drift rendered no divergence despite stored findings: %+v", driftResp.InstancesWithDrift)
	}
	// The node, for the same reason every fleet-wide list needs it: "nginx
	// nginx.conf" is what every host running stock nginx is called, so a finding
	// that does not name the host says "1 of 5 instances" and nothing more.
	foundWeb02 := false
	for _, iw := range driftResp.InstancesWithDrift {
		if iw.NodeDisplayName == "web02" {
			foundWeb02 = true
		}
	}
	if !foundWeb02 {
		t.Errorf("/api/ui/drift rendered a finding without naming the node it is on: %+v", driftResp.InstancesWithDrift)
	}

	var reviewResp pb.DriftReviewResponse
	if err := protojson.Unmarshal(c.get("/api/ui/drift/review/"+itoa(instID)).Body.Bytes(), &reviewResp); err != nil {
		t.Fatalf("decode /api/ui/drift/review/%d: %v", instID, err)
	}
	// The link has to be well formed, not merely present. Handing the finding's
	// raw sql.NullInt64 fields straight to a template put "{5 true}" in the URL,
	// because a struct is always truthy — the equivalent bug here would be a
	// non-empty Link on a Provenance with no FileId.
	foundLink := false
	for _, g := range reviewResp.ObjectGroups {
		for _, f := range g.Findings {
			if f.Provenance != nil && regexp.MustCompile(`^/snapshots/\d+/file/\d+`).MatchString(f.Provenance.Link) {
				foundLink = true
			}
		}
	}
	if !foundLink {
		t.Errorf("the review response carries no usable provenance link: %+v", reviewResp.ObjectGroups)
	}

	// Two more identical hosts, which is a cluster, and a cluster whose members match
	// has no divergences. Landing on /drift with no scope must still show the one
	// instance that did diverge: it is not in any cluster — divergence is what took it
	// out of one — and defaulting to the first cluster returned an empty
	// InstancesWithDrift to an operator who arrived from a nav badge reading "Drift · 1".
	seedNginx(t, db, "web07", "10.90.4.17")
	seedNginx(t, db, "web08", "10.90.4.18")
	if err := db.ReconcileClusters(ctx); err != nil {
		t.Fatal(err)
	}
	if cl, err := db.Clusters(ctx); err != nil || len(cl) == 0 {
		t.Fatalf("two byte-identical hosts formed no cluster (%v), so this test proves nothing", err)
	}
	var landing pb.DriftResponse
	if err := protojson.Unmarshal(c.get("/api/ui/drift").Body.Bytes(), &landing); err != nil {
		t.Fatalf("decode /api/ui/drift: %v", err)
	}
	if len(landing.InstancesWithDrift) == 0 {
		t.Error("/api/ui/drift with no scope landed on a scope with nothing in it while the fleet had a divergence")
	}
}

// The Trace page's Provenance panel puts the configuration lines a rule was
// parsed from on the screen. That is the product's trust mechanism, so it has to
// show real bytes out of the stored snapshot — not just a link that says it
// could. It also has to keep working when the selected rule has no file
// recorded, which is a different branch of the same panel.

// A browser never sends a URL fragment to the server, so reading #b123 there
// meant the provenance highlight could only fire for a request nothing makes:
// every link into a config file landed with nothing marked.
func TestSnapshotFileHighlightsTheByteOffsetFromTheQuery(t *testing.T) {
	s, db := newTestServer(t)
	ctx := t.Context()
	if _, err := db.CreateUser(ctx, "admin", "a good long password", "admin", "Admin", false); err != nil {
		t.Fatal(err)
	}
	seedNginx(t, db, "web02", "10.90.4.11")
	files, err := db.SnapshotFiles(ctx, 1)
	if err != nil || len(files) == 0 {
		t.Fatalf("no snapshot file to read back: %v", err)
	}
	off := strings.Index(testNginx, "server_name")
	if off < 0 {
		t.Fatal("the fixture no longer contains the directive this test anchors on")
	}

	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	// The file viewer is the React SPA now (docs/adr/0017); the byte-offset ->
	// line resolution is asserted against GET /api/ui/snapshots/.../file/...
	// (docs/adr/0018) instead of rendered HTML.
	path := fmt.Sprintf("/api/ui/snapshots/1/file/%d?b=%d", files[0].ID, off)
	w := c.get(path)
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s = %d", path, w.Code)
	}
	var withOffset pb.SnapshotFileResponse
	if err := protojson.Unmarshal(w.Body.Bytes(), &withOffset); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	if !withOffset.HasAnchor || withOffset.LineStart == 0 {
		t.Errorf("no line was resolved (has_anchor=%v line_start=%d), so a provenance link still lands in a file with nothing marked",
			withOffset.HasAnchor, withOffset.LineStart)
	}

	// And without an offset, which is the other branch of the same panel — the one
	// reached by browsing an instance's files rather than following a claim.
	var plain pb.SnapshotFileResponse
	if err := protojson.Unmarshal(c.get(fmt.Sprintf("/api/ui/snapshots/1/file/%d", files[0].ID)).Body.Bytes(), &plain); err != nil {
		t.Fatalf("decode file with no offset: %v", err)
	}
	if plain.HasAnchor {
		t.Error("a file opened with no provenance offset resolved an anchor anyway")
	}
}

