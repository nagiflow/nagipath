package web

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
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

// login is the SPA's real login request shape: JSON to /api/login
// (internal/api/auth.go) rather than the old form-encoded /login redirect.
func (c *client) login(username, password string) *httptest.ResponseRecorder {
	c.t.Helper()
	return c.postJSON("/api/login", map[string]any{"username": username, "password": password})
}

// bootstrapAdmin is the SPA's real first-run request shape: JSON to
// /api/setup (internal/api/auth.go). Validation behavior (short password,
// mismatch, second-setup-403) is covered in internal/api/auth_test.go now
// that setup is a JSON endpoint rather than a template with a flash-message
// redirect — this just proves the one happy path every other test here needs.
func (c *client) bootstrapAdmin(t *testing.T, username, password string) {
	t.Helper()
	w := c.postJSON("/api/setup", map[string]any{"username": username, "password": password, "confirm": password})
	if w.Code != http.StatusOK || c.cookie == "" {
		t.Fatalf("bootstrap setup for %q = %d, cookie %q", username, w.Code, c.cookie)
	}
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
	// /setup is the SPA shell now (docs/adr/0017, Phase 8) — it renders
	// client-side, so this only proves the Go route serves it at all.
	if w := c.get("/setup"); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `id="root"`) {
		t.Fatalf("GET /setup did not serve the SPA shell: %d", w.Code)
	}

	// Setup validation (short password, mismatch, second-setup-403) is exercised
	// in internal/api/auth_test.go now that it's a JSON endpoint; this just
	// proves the one happy path every other test in this file depends on.
	c.bootstrapAdmin(t, "admin", "a good long password")
	if n, _ := db.UserCount(t.Context()); n != 1 {
		t.Fatalf("users after setup = %d, want 1", n)
	}

	// /password is the SPA shell too, behind s.auth like every other
	// authenticated page.
	if w := c.get("/password"); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `id="root"`) {
		t.Fatalf("GET /password did not serve the SPA shell: %d", w.Code)
	}

	// Every page is the SPA shell now (docs/adr/0017, Phase 9 — Import
	// inventory was the last holdout). Per-page content assertions live with
	// each page's own SPA-shell test (TestDashboardServesSPAShell,
	// TestNodesServesSPAShell, etc.); this just confirms the last one to move,
	// /nodes/import, serves the same shell as everything else.
	if w := c.get("/nodes/import"); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `id="root"`) {
		t.Fatalf("GET /nodes/import did not serve the SPA shell: %d", w.Code)
	}
}

// The Content-Security-Policy sets script-src 'self' with no 'unsafe-inline', so
// an inline handler or an inline <script> in a template does not misbehave — it
// does not run at all. That failure is silent: the markup looks right, the button
// renders, and clicking it does nothing. Three shipped controls were dead this way
// (a confirm() on an irreversible node delete, one on disabling a user, and three
// filter selects that submitted nothing), which is why this is a test and not a
// review note.
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

// A 404 has to send a real 404 status and still hand the browser a working
// app: the operator gets here by mistyping a URL or by following a link to a
// node someone removed, and both cases need a way back. The SPA's wildcard
// route renders the actual "not found" content (path shown, link home) —
// that's client-side, so it's outside what an httptest.Recorder can see; this
// pins down the Go-level contract the React NotFoundPage depends on.
func TestNotFoundIsAPageInsideTheShell(t *testing.T) {
	s, db := newTestServer(t)
	if _, err := db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false); err != nil {
		t.Fatal(err)
	}
	c := &client{t: t, s: s}
	c.login("admin", "a good long password")

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
		if !strings.Contains(w.Body.String(), `id="root"`) {
			t.Errorf("GET %s did not serve the SPA shell for the wildcard route to render:\n%s", path, w.Body.String())
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
	c.login("admin", "a good long password")
	if c.cookie == "" {
		t.Fatal("login did not set a session cookie")
	}

	// Adding a node is the SPA now (docs/adr/0017): POST /api/nodes
	// (internal/api/nodes.go), CSRF checked via header only (middleware.go).
	saved := c.csrf
	c.csrf = ""
	if got := c.postJSON("/api/nodes", map[string]any{"address": "10.0.0.1"}).Code; got != http.StatusForbidden {
		t.Errorf("POST without a CSRF token = %d, want 403", got)
	}
	c.csrf = "forged-token"
	if got := c.postJSON("/api/nodes", map[string]any{"address": "10.0.0.1"}).Code; got != http.StatusForbidden {
		t.Errorf("POST with a forged CSRF token = %d, want 403", got)
	}
	c.csrf = saved
	if got := c.postJSON("/api/nodes", map[string]any{"address": "10.0.0.1"}).Code; got != http.StatusOK {
		t.Errorf("POST with the right CSRF token = %d, want 200", got)
	}
	nodes, _ := db.Nodes(t.Context())
	if len(nodes) != 1 {
		t.Errorf("nodes = %d, want 1", len(nodes))
	}
}

// POST /api/logout is s.requireAuth-wrapped like every other internal/api
// mutation, so its CSRF check is inherited rather than hand-rolled — this
// pins down that it's still enforced, and that a forced-password-change user
// can still reach it: requireAuth (unlike internal/web's auth() middleware,
// which still gates every other page) has no must-change-password special
// case, so it never redirects this request to /password before logout runs.
func TestLogoutRequiresCSRFAndWorksMidForcedPasswordChange(t *testing.T) {
	s, db := newTestServer(t)
	if _, err := db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", true); err != nil {
		t.Fatal(err)
	}
	c := &client{t: t, s: s}
	c.login("admin", "a good long password")
	if c.cookie == "" {
		t.Fatal("login did not set a session cookie")
	}

	saved := c.csrf
	c.csrf = ""
	if got := c.postJSON("/api/logout", nil).Code; got != http.StatusForbidden {
		t.Errorf("POST /api/logout without a CSRF token = %d, want 403", got)
	}
	c.csrf = "forged-token"
	if got := c.postJSON("/api/logout", nil).Code; got != http.StatusForbidden {
		t.Errorf("POST /api/logout with a forged CSRF token = %d, want 403", got)
	}
	sessionBeforeLogout := c.cookie
	c.csrf = saved
	w := c.postJSON("/api/logout", nil)
	if w.Code != http.StatusOK {
		t.Errorf("POST /api/logout with the right CSRF token = %d, want 200", w.Code)
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
	c.login("admin", "a good long password")

	w := c.postJSON("/api/nodes", map[string]any{"address": "10.90.4.0/24"})
	// The machine code is the gRPC status's own name (invalid_argument) now
	// that AddNode is a NodeService RPC (gateway.go's gatewayError) rather
	// than a bespoke "no_scanning" string — coarser, but the message text
	// still says why, which is what a human reads.
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "does not scan networks") {
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
	c.login("admin", "a good long password")

	w := c.postJSON("/api/settings/users", map[string]any{
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
	if err := protojson.Unmarshal(c.get("/api/settings/users").Body.Bytes(), &list); err != nil {
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
	w = c2.login("newviewer", "a good long password")
	if w.Code != http.StatusOK || c2.cookie == "" {
		t.Fatalf("the new user could not sign in: %d", w.Code)
	}
	if got := c2.get("/api/settings/users").Code; got != http.StatusForbidden {
		t.Errorf("a viewer reading /settings/users = %d, want 403", got)
	}
	if got := c2.postJSON("/api/settings/users", map[string]any{
		"username": "x", "password": "a good long password", "confirm": "a good long password", "role": "viewer",
	}).Code; got != http.StatusForbidden {
		t.Errorf("a viewer creating a user = %d, want 403", got)
	}

	// A short password is refused rather than accepted quietly, the same as /setup.
	w = c.postJSON("/api/settings/users", map[string]any{
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
	c.login("admin", "a good long password")

	w := c.postJSON("/api/settings/users/"+strconv.FormatInt(viewerID, 10)+"/disable", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("disabling the viewer failed: %d %s", w.Code, w.Body.String())
	}

	c2 := &client{t: t, s: s}
	w = c2.login("viewer", "a good long password")
	if c2.cookie != "" || w.Code < 400 {
		t.Fatal("a disabled user was able to sign in")
	}

	// Re-enabling restores it.
	c.postJSON("/api/settings/users/"+strconv.FormatInt(viewerID, 10)+"/enable", nil)
	c3 := &client{t: t, s: s}
	c3.login("viewer", "a good long password")
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
	c.login("admin", "a good long password")

	w := c.postJSON("/api/settings/users/"+strconv.FormatInt(adminID, 10)+"/disable", nil)
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
	w = c.postJSON("/api/settings/users/"+strconv.FormatInt(adminID, 10)+"/disable", nil)
	if w.Code != http.StatusOK {
		t.Errorf("disabling one of two admins was refused: %d %s", w.Code, w.Body.String())
	}
}

// Login rate limiting (throttled per username, checked before the
// deliberately slow password hash runs, off when the limit is 0) is business
// logic that now lives in internal/api/auth.go's postLogin — see
// TestLoginRateLimitBlocksThenClearsAfterTheWindow in internal/api/auth_test.go.

func TestViewerCannotChangeAnything(t *testing.T) {
	s, db := newTestServer(t)
	db.CreateUser(t.Context(), "viewer", "a good long password", "viewer", "Viewer", false)
	c := &client{t: t, s: s}
	c.login("viewer", "a good long password")
	if c.cookie == "" {
		t.Fatal("viewer could not sign in")
	}
	if got := c.get("/nodes").Code; got != http.StatusOK {
		t.Errorf("a viewer must still be able to read /nodes, got %d", got)
	}
	if got := c.postJSON("/api/nodes", map[string]any{"address": "10.0.0.9"}).Code; got != http.StatusForbidden {
		t.Errorf("viewer adding a node = %d, want 403", got)
	}
	if got := c.get("/api/settings/audit").Code; got != http.StatusOK {
		t.Errorf("viewer reading the audit log = %d, want 200", got)
	}
}

func TestMustChangePasswordBlocksEverythingElse(t *testing.T) {
	s, db := newTestServer(t)
	db.CreateUser(t.Context(), "admin", "a temporary password", "admin", "Admin", true)
	c := &client{t: t, s: s}
	c.login("admin", "a temporary password")

	w := c.get("/nodes")
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/password" {
		t.Fatalf("GET /nodes = %d -> %q, want a redirect to /password", w.Code, w.Header().Get("Location"))
	}
	if got := c.get("/password").Code; got != http.StatusOK {
		t.Fatalf("GET /password = %d", got)
	}
	// No current password is asked for, because the temporary one was assigned.
	if got := c.postJSON("/api/password", map[string]any{
		"new": "a properly chosen password", "confirm": "a properly chosen password",
	}).Code; got != http.StatusOK {
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
	c.login("admin", "a good long password")

	w := c.postJSON("/api/settings/credentials", map[string]any{
		"name": "lab", "username": "nagipath", "privateKey": testKey,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("storing the credential failed: %d %s", w.Code, w.Body.String())
	}
	creds, _ := db.Credentials(t.Context())
	if len(creds) != 1 {
		t.Fatalf("credentials = %d, want 1", len(creds))
	}
	body := c.get("/api/settings/credentials").Body.String()
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
	c.login("admin", "a good long password")

	var empty pb.TraceResponse
	if err := protojson.Unmarshal(c.get("/api/trace").Body.Bytes(), &empty); err != nil {
		t.Fatal(err)
	}
	if !empty.Empty {
		t.Error("the trace response should say why it cannot trace anything")
	}
	// A trace against an empty fleet must render a result, not a 500.
	w := c.get("/api/trace?scheme=https&hostname=shop.example.com&path=/")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/trace on an empty fleet = %d\n%s", w.Code, w.Body.String())
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
	w = c.get("/api/trace?url=" + url.QueryEscape("shop.example.com/v2/charge"))
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
	c.login("admin", "a good long password")
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
	c.login("admin", "a good long password")
	// Node, not instance: the tabs hang off the node with the process in the
	// query string.
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
	// Sites is the SPA now (docs/adr/0017); the CSV export moved with it to
	// GET /api/sites?export=csv (internal/api/sites.go).
	if w := c.get("/api/sites?export=csv"); w.Code != http.StatusOK ||
		!strings.Contains(w.Header().Get("Content-Type"), "text/csv") ||
		!strings.Contains(w.Body.String(), "shop.example.com") {
		t.Errorf("GET /api/sites?export=csv did not return the filtered site export: %d %q", w.Code, w.Body.String())
	}

	// Search is the SPA now (docs/adr/0017, Phase 5); GET /api/search
	// (internal/api/search.go) returns a typed proto response, so the class of
	// bug this used to guard — a template reading a field store.TextHit does
	// not have — cannot recur: a mismatched field fails to compile.
	if w := c.get("/api/search?q=proxy_pass"); w.Code != http.StatusOK {
		t.Errorf("GET /api/search?q=proxy_pass = %d\n%s", w.Code, w.Body.String())
	}

	// A POST stores the Trace, and the bare GET then lists it. That table was
	// only ever rendered against an empty history, so a field it read that
	// trace.Summary does not have reached a real install as a 500.
	if w := c.postJSON("/api/trace", map[string]any{"url": "shop.example.com/api/v2"}); w.Code != http.StatusOK {
		t.Fatalf("POST /api/trace = %d\n%s", w.Code, w.Body.String())
	}
	var recent pb.TraceResponse
	recentW := c.get("/api/trace")
	if recentW.Code != http.StatusOK {
		t.Fatalf("GET /api/trace with a stored trace = %d\n%s", recentW.Code, recentW.Body.String())
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
	// per tab are field assertions against GET /api/nodes/{id}[/{tab}] instead.
	nodeAPI := "/api/nodes/" + strconv.FormatInt(nodeID, 10)

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
	c.login("admin", "a good long password")

	// Clusters is the React SPA now (docs/adr/0017); the fact it used to read off
	// rendered HTML is asserted against GET /api/clusters instead, decoded via
	// protojson against the generated schema (docs/adr/0018). One cluster, two
	// members, vendor nginx, the third (different-vendor) host excluded.
	getClusters := func() *pb.ClustersResponse {
		t.Helper()
		got := &pb.ClustersResponse{}
		if err := protojson.Unmarshal(c.get("/api/clusters").Body.Bytes(), got); err != nil {
			t.Fatalf("decode /api/clusters: %v", err)
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
	c.login("admin", "a good long password")

	// Drift is the React SPA now (docs/adr/0017); the same guarantees are
	// asserted against GET /api/drift and /api/drift/review/{id}
	// (docs/adr/0018) instead of rendered HTML.
	var driftResp pb.DriftResponse
	if err := protojson.Unmarshal(c.get("/api/drift?cluster=all").Body.Bytes(), &driftResp); err != nil {
		t.Fatalf("decode /api/drift?cluster=all: %v", err)
	}
	// /drift is the summary: which nodes are off baseline and by how much. The
	// findings themselves live one click deeper, on the review screen.
	if len(driftResp.InstancesWithDrift) == 0 {
		t.Errorf("/api/drift rendered no divergence despite stored findings: %+v", driftResp.InstancesWithDrift)
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
		t.Errorf("/api/drift rendered a finding without naming the node it is on: %+v", driftResp.InstancesWithDrift)
	}

	var reviewResp pb.DriftReviewResponse
	if err := protojson.Unmarshal(c.get("/api/drift/review/"+strconv.FormatInt(instID, 10)).Body.Bytes(), &reviewResp); err != nil {
		t.Fatalf("decode /api/drift/review/%d: %v", instID, err)
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
	if err := protojson.Unmarshal(c.get("/api/drift").Body.Bytes(), &landing); err != nil {
		t.Fatalf("decode /api/drift: %v", err)
	}
	if len(landing.InstancesWithDrift) == 0 {
		t.Error("/api/drift with no scope landed on a scope with nothing in it while the fleet had a divergence")
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
	c.login("admin", "a good long password")

	// The file viewer is the React SPA now (docs/adr/0017); the byte-offset ->
	// line resolution is asserted against GET /api/snapshots/.../file/...
	// (docs/adr/0018) instead of rendered HTML.
	path := fmt.Sprintf("/api/snapshots/1/file/%d?b=%d", files[0].ID, off)
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
	if err := protojson.Unmarshal(c.get(fmt.Sprintf("/api/snapshots/1/file/%d", files[0].ID)).Body.Bytes(), &plain); err != nil {
		t.Fatalf("decode file with no offset: %v", err)
	}
	if plain.HasAnchor {
		t.Error("a file opened with no provenance offset resolved an anchor anyway")
	}
}
