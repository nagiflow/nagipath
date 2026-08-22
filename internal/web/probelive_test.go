package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/nagiflow/nagipath/internal/probe"
)

// The one decision in the live-run registry: verified is final. A hop proved it
// handled the request, so a later step on the same hop — a second log path that did
// not match, or a stale "checking" — must not take the green ring off it.
func TestProbeRunStateVerifiedIsFinal(t *testing.T) {
	r := &probeRun{state: map[int]string{}}
	for _, s := range []probe.Step{
		{Ordinal: -1, Text: "GET https://shop.example.com/v2/charge"},
		{Ordinal: 0, State: probe.StateChecking},
		{Ordinal: 0, State: probe.StateVerified},
		{Ordinal: 0, State: probe.StateMissing},
		{Ordinal: 1, State: probe.StateChecking},
		{Ordinal: 1, State: probe.StateMissing},
	} {
		r.add(s)
	}
	v := r.view()
	if v.State[0] != probe.StateVerified {
		t.Errorf("hop 0 = %q, want %q", v.State[0], probe.StateVerified)
	}
	if v.State[1] != probe.StateMissing {
		t.Errorf("hop 1 = %q, want %q", v.State[1], probe.StateMissing)
	}
	if _, ok := v.State[-1]; ok {
		t.Error("a step that is not about a hop put a state on the graph")
	}
	if len(v.Steps) != 6 {
		t.Errorf("view kept %d steps, want all 6", len(v.Steps))
	}
	// The view is a copy: a render must not see the goroutine's map change mid-page.
	r.add(probe.Step{Ordinal: 1, State: probe.StateVerified})
	if v.State[1] != probe.StateMissing || len(v.Steps) != 6 {
		t.Error("view() aliases the run instead of copying it")
	}
}

// Demo mode is the equivalent of PRD-V1.md §8's "Collection hard-disabled by flag"
// for Probe: a public trial instance must never send a real outbound request.
// The refusal has to happen before the Probe row (and therefore before the request)
// exists at all — a probe count of zero afterward is the proof, not just the redirect.
func TestProbeDemoModeRefusesBeforeAnythingIsStored(t *testing.T) {
	s, db := newTestServer(t)
	db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false)
	s.DemoMode = true
	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	w := c.post("/trace/probe", url.Values{"url": {"https://shop.example.com/v2/charge"}, "method": {"GET"}})
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "err=") || !strings.Contains(loc, "demo") {
		t.Fatalf("demo mode did not refuse the probe: redirect %q", loc)
	}

	var n int
	if err := db.R.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM probe`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("a probe row was stored despite demo mode, count=%d", n)
	}

	events, err := db.AuditEvents(t.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range events {
		if e.Action == "probe.run" && e.Outcome == "denied" {
			found = true
		}
	}
	if !found {
		t.Error("the demo-mode refusal was not audited")
	}
}

// The rate limit has to block before the request goes out, exactly like demo mode —
// a probe row appearing after the block would mean the guardrail raced the request
// instead of preceding it.
func TestProbeRateLimitBlocksThenAllowsAfterTheWindow(t *testing.T) {
	s, db := newTestServer(t)
	db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false)
	ctx := t.Context()
	users, err := db.Users(ctx)
	if err != nil || len(users) != 1 {
		t.Fatalf("Users() = %v, %v", users, err)
	}
	actorID := users[0].ID

	if err := db.SetSetting(ctx, "probe_rate_limit_max", "2", nil); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSetting(ctx, "probe_rate_limit_window_seconds", "60", nil); err != nil {
		t.Fatal(err)
	}

	// The Entry Point key is whatever startTraceProbe would compute for this pasted
	// URL, so the pre-seeded rows land in exactly the bucket the handler will check.
	q, err := parseTarget("https://shop.example.com/v2/charge")
	if err != nil {
		t.Fatal(err)
	}
	target := targetURL(q)
	seedProbe := func(age time.Duration, token string) {
		t.Helper()
		when := time.Now().UTC().Add(-age).Format("2006-01-02T15:04:05Z")
		if _, err := db.W.ExecContext(ctx, `INSERT INTO probe
			(actor_user_id, method, url, correlation_token, origin_host, requested_at, result)
			VALUES (?,?,?,?,?,?,?)`,
			actorID, "GET", target, token, "test", when, "completed"); err != nil {
			t.Fatal(err)
		}
	}
	seedProbe(30*time.Second, "tok-1")
	seedProbe(10*time.Second, "tok-2")

	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	w := c.post("/trace/probe", url.Values{"url": {target}, "method": {"GET"}})
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "err=") || !strings.Contains(loc, "rate") {
		t.Fatalf("the third probe within the window was not blocked: redirect %q", loc)
	}
	var n int
	if err := db.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM probe`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("a probe row was stored despite the rate limit, count=%d, want 2", n)
	}

	// Age the seeded rows past the window: the guardrail clears and the request is
	// allowed through again (it is free to fail on the network from here — that is
	// not the rate limiter's concern).
	if _, err := db.W.ExecContext(ctx, `UPDATE probe SET requested_at = ? WHERE actor_user_id = ?`,
		time.Now().UTC().Add(-90*time.Second).Format("2006-01-02T15:04:05Z"), actorID); err != nil {
		t.Fatal(err)
	}
	w = c.post("/trace/probe", url.Values{"url": {target}, "method": {"GET"}})
	loc = w.Header().Get("Location")
	if strings.Contains(loc, "rate") {
		t.Errorf("the rate limit still blocked after the window passed: redirect %q", loc)
	}
}

// Neither guardrail should touch the ordinary path: an admin, under the limit, on a
// production instance, still gets redirected to a running Probe.
func TestProbeNormalPathStillReachesTheHandler(t *testing.T) {
	s, db := newTestServer(t)
	db.CreateUser(t.Context(), "admin", "a good long password", "admin", "Admin", false)
	c := &client{t: t, s: s}
	c.post("/login", url.Values{"username": {"admin"}, "password": {"a good long password"}})

	// Port 1 on loopback refuses instantly, so the background request this kicks off
	// fails fast instead of hanging past the end of the test.
	w := c.post("/trace/probe", url.Values{"url": {"http://127.0.0.1:1/x"}, "method": {"GET"}})
	if w.Code != http.StatusSeeOther {
		t.Fatalf("POST /trace/probe = %d, want %d", w.Code, http.StatusSeeOther)
	}
	loc := w.Header().Get("Location")
	if strings.Contains(loc, "err=") {
		t.Fatalf("the normal path was refused: redirect %q", loc)
	}
	if !strings.Contains(loc, "run=") {
		t.Fatalf("the normal path did not start a run: redirect %q", loc)
	}

	var n int
	if err := db.R.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM probe`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		// insertProbe runs in the background goroutine; seeing it land already just
		// means the loopback refusal was very fast, which is fine — the point is
		// that nothing here refused the request up front.
		t.Logf("probe row already recorded (n=%d) — background goroutine ran ahead", n)
	}
}
