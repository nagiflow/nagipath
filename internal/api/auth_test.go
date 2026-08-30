package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

// authClient is internal/web/web_test.go's client, trimmed to what
// login/setup/logout/password need: a cookie and a CSRF token carried across
// requests, the way a browser (or ui/src/api/client.ts) does. internal/api
// doesn't import internal/web, so this is its own copy rather than a shared
// helper — the one place in this package's test suite that needs real
// HTTP-level plumbing (cookies, CSRF headers) instead of calling a handler
// directly, because the cookie/CSRF/rate-limit behavior is the thing under
// test here.
type authClient struct {
	t      *testing.T
	s      *Server
	cookie string
	csrf   string
}

func (c *authClient) get(path string) *httptest.ResponseRecorder {
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

func (c *authClient) postJSON(path string, body any) *httptest.ResponseRecorder {
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

func (c *authClient) absorb(w *httptest.ResponseRecorder) {
	for _, ck := range w.Result().Cookies() {
		if ck.Name == cookieName && ck.Value != "" {
			c.cookie = ck.Value
			c.csrf = csrfToken(ck.Value)
		}
	}
}

func decodeSession(t *testing.T, w *httptest.ResponseRecorder) *pb.SessionResponse {
	t.Helper()
	var resp pb.SessionResponse
	if err := protojson.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode session response: %v (body %q)", err, w.Body.String())
	}
	return &resp
}

// A short password, a mismatched confirmation, and a second setup attempt
// must all be refused — and refused without creating a user — while the
// actual happy path issues a working session. Ported from internal/web's old
// TestFirstRunThenEveryPageRenders, which asserted this via a redirect's
// ?err= query param before setup became a JSON endpoint (Phase 8).
func TestSetupValidation(t *testing.T) {
	db := testDB(t)
	s := New(db, nil, false)
	c := &authClient{t: t, s: s}

	if w := c.postJSON("/setup", map[string]any{"username": "admin", "password": "short", "confirm": "short"}); w.Code != http.StatusUnprocessableEntity {
		t.Errorf("short password = %d, want 422", w.Code)
	}
	if n, _ := db.UserCount(t.Context()); n != 0 {
		t.Fatal("a user was created despite the rejected password")
	}

	if w := c.postJSON("/setup", map[string]any{
		"username": "admin", "password": "a good long password", "confirm": "a different long password",
	}); w.Code != http.StatusUnprocessableEntity {
		t.Errorf("mismatched confirm = %d, want 422", w.Code)
	}
	if n, _ := db.UserCount(t.Context()); n != 0 {
		t.Fatal("a user was created despite the mismatched confirmation")
	}

	w := c.postJSON("/setup", map[string]any{
		"username": "admin", "password": "a good long password", "confirm": "a good long password",
	})
	if w.Code != http.StatusOK || c.cookie == "" {
		t.Fatalf("setup = %d, cookie %q", w.Code, c.cookie)
	}
	resp := decodeSession(t, w)
	if !resp.Authenticated || resp.User.Username != "admin" || resp.User.Role != "admin" {
		t.Errorf("session after setup = %+v, want authenticated admin", resp)
	}

	// Setup must close permanently once an admin exists.
	if got := c.postJSON("/setup", map[string]any{
		"username": "second", "password": "another long password", "confirm": "another long password",
	}).Code; got != http.StatusForbidden {
		t.Errorf("second setup = %d, want 403", got)
	}
}

// The message is deliberately identical for a bad password and a missing
// user: which one it was is not the caller's business to learn.
func TestLoginGenericFailureMessage(t *testing.T) {
	db := testDB(t)
	db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false)
	s := New(db, nil, false)

	for _, body := range []map[string]any{
		{"username": "admin", "password": "wrong password"},
		{"username": "no-such-user", "password": "a good long password"},
	} {
		c := &authClient{t: t, s: s}
		w := c.postJSON("/login", body)
		if w.Code != http.StatusUnauthorized || c.cookie != "" {
			t.Errorf("login %+v = %d, want 401 and no cookie", body, w.Code)
		}
		var errBody struct {
			Error struct{ Message string } `json:"error"`
		}
		json.Unmarshal(w.Body.Bytes(), &errBody)
		if errBody.Error.Message != "invalid username or password" {
			t.Errorf("login %+v message = %q, want the generic message", body, errBody.Error.Message)
		}
	}
}

// Failed logins are throttled per username, checked before the (deliberately
// slow) password hash runs, and a limit of 0 turns the guardrail off. Ported
// from internal/web's old TestLoginRateLimitBlocksThenClearsAfterTheWindow.
func TestLoginRateLimitBlocksThenClearsAfterTheWindow(t *testing.T) {
	db := testDB(t)
	db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false)
	if err := db.SetSetting(t.Context(), "login_rate_limit_max", "3", nil); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSetting(t.Context(), "login_rate_limit_window_seconds", "300", nil); err != nil {
		t.Fatal(err)
	}
	s := New(db, nil, false)

	c := &authClient{t: t, s: s}
	for i := range 3 {
		w := c.postJSON("/login", map[string]any{"username": "admin", "password": "wrong password"})
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: wrong password = %d, want 401", i, w.Code)
		}
	}
	// The ceiling is reached: even the correct password is refused now, without
	// ever reaching Authenticate.
	w := c.postJSON("/login", map[string]any{"username": "admin", "password": "a good long password"})
	if c.cookie != "" {
		t.Fatal("login succeeded despite the rate limit")
	}
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("rate-limited login = %d, want 429", w.Code)
	}

	// A different username is not caught by the same limit.
	db.CreateUser(t.Context(), "someoneelse", "a good long password", "viewer", "Someone", false)
	c2 := &authClient{t: t, s: s}
	c2.postJSON("/login", map[string]any{"username": "someoneelse", "password": "a good long password"})
	if c2.cookie == "" {
		t.Error("a different username was blocked by another account's rate limit")
	}

	// Once the window has passed, the correct password works again.
	if _, err := db.W.ExecContext(t.Context(),
		`UPDATE audit_event SET at = ? WHERE action = 'auth.login' AND target_label = 'admin'`,
		time.Now().UTC().Add(-10*time.Minute).Format("2006-01-02T15:04:05Z")); err != nil {
		t.Fatal(err)
	}
	c.postJSON("/login", map[string]any{"username": "admin", "password": "a good long password"})
	if c.cookie == "" {
		t.Error("login after the rate-limit window passed should have succeeded")
	}

	// A limit of 0 disables the guardrail rather than blocking everything.
	if err := db.SetSetting(t.Context(), "login_rate_limit_max", "0", nil); err != nil {
		t.Fatal(err)
	}
	c3 := &authClient{t: t, s: s}
	for range 5 {
		c3.postJSON("/login", map[string]any{"username": "admin", "password": "wrong password"})
	}
	c3.postJSON("/login", map[string]any{"username": "admin", "password": "a good long password"})
	if c3.cookie == "" {
		t.Error("login_rate_limit_max=0 should disable the guardrail")
	}
}

// A short new password and a mismatched confirmation are refused. A regular
// change requires the current password (so a borrowed logged-in browser
// can't lock the real owner out); a forced first-login change does not,
// since the temporary password was handed out by an admin. Either way,
// changing the password ends every other session.
func TestPasswordChangeRequiresCurrentUnlessForced(t *testing.T) {
	db := testDB(t)
	db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false)
	s := New(db, nil, false)

	c := &authClient{t: t, s: s}
	c.postJSON("/login", map[string]any{"username": "admin", "password": "a good long password"})
	if c.cookie == "" {
		t.Fatal("login did not set a session cookie")
	}
	// A second session for the same user, to prove it gets ended below.
	c2 := &authClient{t: t, s: s}
	c2.postJSON("/login", map[string]any{"username": "admin", "password": "a good long password"})

	if w := c.postJSON("/password", map[string]any{"current": "a good long password", "new": "short", "confirm": "short"}); w.Code != http.StatusUnprocessableEntity {
		t.Errorf("short new password = %d, want 422", w.Code)
	}
	if w := c.postJSON("/password", map[string]any{
		"current": "a good long password", "new": "a properly chosen password", "confirm": "a different one",
	}); w.Code != http.StatusUnprocessableEntity {
		t.Errorf("mismatched confirm = %d, want 422", w.Code)
	}
	if w := c.postJSON("/password", map[string]any{
		"current": "the wrong password", "new": "a properly chosen password", "confirm": "a properly chosen password",
	}); w.Code != http.StatusUnprocessableEntity {
		t.Errorf("wrong current password = %d, want 422", w.Code)
	}

	w := c.postJSON("/password", map[string]any{
		"current": "a good long password", "new": "a properly chosen password", "confirm": "a properly chosen password",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("password change = %d, want 200", w.Code)
	}
	resp := decodeSession(t, w)
	if resp.MustChangePassword {
		t.Error("must_change_password should be false right after changing it")
	}

	// The other session was ended by the change.
	if got := c2.get("/session"); func() bool {
		s := decodeSession(t, got)
		return s.Authenticated
	}() {
		t.Error("the other session should have been ended by the password change")
	}

	// A forced first-login change needs no current password.
	db.CreateUser(t.Context(), "temp", "a temporary password", "viewer", "Temp", true)
	c3 := &authClient{t: t, s: s}
	c3.postJSON("/login", map[string]any{"username": "temp", "password": "a temporary password"})
	if w := c3.postJSON("/password", map[string]any{
		"new": "a properly chosen password", "confirm": "a properly chosen password",
	}); w.Code != http.StatusOK {
		t.Errorf("forced password change = %d, want 200 (no current password required)", w.Code)
	}
}
