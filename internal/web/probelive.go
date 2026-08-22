package web

import (
	"context"
	"fmt"
	"net/http"
	neturl "net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nagiflow/nagipath/internal/probe"
	"github.com/nagiflow/nagipath/internal/trace"
)

// A live Probe on the Trace screen. The Trace itself is answered from collected
// configuration; this is the operator asking nagipath to send one real request and
// read the access log on every hop, which is the only thing that can raise a hop to
// Verified. It stays operator-triggered and audited (probe.md §6) — the graph
// following along does not make it a background caller.
//
// ponytail: runs live in memory and a meta refresh is the whole transport, as on the
// collection screen. A probe is a few seconds of progress for one operator, so a
// restart mid-probe is a re-run, not lost data — the evidence itself is already in
// the database. Upgrade path is SSE plus a fragment swap when a hop count makes the
// one-second full reload feel slow.
type probeRun struct {
	ID      int64
	TraceID int64
	URL     string
	Method  string

	mu    sync.Mutex
	steps []probe.Step
	state map[int]string
	done  bool
	err   string
}

// runView is the snapshot the template reads. Copied under the lock so a render
// cannot race the goroutine still appending to it.
type runView struct {
	ID     int64
	URL    string
	Method string
	Steps  []probe.Step
	// State maps a hop ordinal to what the graph shows for it.
	State map[int]string
	Done  bool
	// Err carries a failure that never reached the database — a URL nagipath will not
	// send, a method that would change something. Anything that did get stored is read
	// back from its own rows instead.
	Err string
}

func (r *probeRun) add(s probe.Step) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.steps = append(r.steps, s)
	if s.Ordinal >= 0 && s.State != "" {
		// Verified is final: a later "checking" on the same hop must not take the ring
		// off a hop that already proved it handled the request.
		if r.state[s.Ordinal] != probe.StateVerified {
			r.state[s.Ordinal] = s.State
		}
	}
}

func (r *probeRun) view() *runView {
	r.mu.Lock()
	defer r.mu.Unlock()
	v := &runView{ID: r.ID, URL: r.URL, Method: r.Method, Done: r.done, Err: r.err,
		State: map[int]string{}, Steps: append([]probe.Step(nil), r.steps...)}
	for k, s := range r.state {
		v.State[k] = s
	}
	return v
}

func (r *probeRun) finish(res *probe.Result, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.done = true
	if err != nil {
		r.err = err.Error()
	} else if res != nil && res.Err != "" {
		r.err = res.Err
	}
}

// probeRunView returns the run a Trace page was asked to show, or nil.
func (s *Server) probeRunView(id int64) *runView {
	if id == 0 {
		return nil
	}
	s.probeMu.Lock()
	run := s.probeRuns[id]
	s.probeMu.Unlock()
	if run == nil {
		return nil
	}
	return run.view()
}

// startTraceProbe sends the request and follows the path. It stores the Trace first
// when the page did not come from a POST: a Probe attaches its evidence to hop rows,
// so there has to be a Trace for it to attach to.
func (s *Server) startTraceProbe(w http.ResponseWriter, r *http.Request) {
	ctx, u := r.Context(), userOf(r)
	q, err := parseTarget(r.FormValue("url"))
	if err != nil {
		redirect(w, r, "/trace", "", err.Error())
		return
	}
	method := strings.ToUpper(strings.TrimSpace(r.FormValue("method")))
	if method != "HEAD" {
		method = "GET"
	}
	traceID, _ := strconv.ParseInt(r.FormValue("trace"), 10, 64)

	// The link back already carries a query, so results are appended with & — one
	// more `?` produces a URL that reads as a literal parameter value.
	target := targetURL(q)
	link := "/trace?method=" + method + "&url=" + neturl.QueryEscape(target)
	fail := func(msg string) {
		http.Redirect(w, r, link+"&err="+neturl.QueryEscape(msg), http.StatusSeeOther)
	}

	// Both guardrails below refuse before anything is stored or sent: a demo
	// instance or a rate-limited operator gets nothing on the wire, not a request
	// that races a check (probe.md §2). Denied here still gets an audit_event,
	// because a refused Probe is still something an operator asked nagipath to do.
	if s.DemoMode {
		s.DB.AuditDetail(ctx, &u.ID, "probe.run", "probe", nil, target,
			map[string]any{"method": method, "reason": "demo_mode"}, "denied", remoteAddr(r))
		fail("this is a demo instance; NAGIPATH_DEMO_MODE refuses every probe")
		return
	}
	if limited, err := probe.RateLimited(ctx, s.DB, u.ID, target); err != nil {
		fail(err.Error())
		return
	} else if limited {
		s.DB.AuditDetail(ctx, &u.ID, "probe.run", "probe", nil, target,
			map[string]any{"method": method, "reason": "rate_limited"}, "denied", remoteAddr(r))
		fail(fmt.Sprintf("rate limit reached: %d probe(s) already sent to this entry point in the last %ds by this operator",
			s.DB.SettingInt(ctx, "probe_rate_limit_max"), s.DB.SettingInt(ctx, "probe_rate_limit_window_seconds")))
		return
	}

	if traceID == 0 {
		top, err := trace.Load(ctx, s.DB)
		if err != nil {
			fail(err.Error())
			return
		}
		id, err := trace.Save(ctx, s.DB, trace.Walk(top, q), nil)
		if err != nil {
			fail("the trace could not be stored, so a probe has nothing to attach evidence to: " + err.Error())
			return
		}
		traceID = id
	}

	run := &probeRun{TraceID: traceID, URL: target, Method: method, state: map[int]string{}}
	s.probeMu.Lock()
	s.probeSeq++
	run.ID = s.probeSeq
	if s.probeRuns == nil {
		s.probeRuns = map[int64]*probeRun{}
	}
	s.probeRuns[run.ID] = run
	s.probeMu.Unlock()

	p := &probe.Prober{DB: s.DB, Dialer: s.collector.Dialer, Log: s.Log, Version: Version}
	opts := probe.Options{TraceID: traceID, URL: target, Method: method,
		MaxRedirects: s.DB.SettingInt(ctx, "probe_max_redirects"),
		ActorID:      u.ID, OriginHost: remoteAddr(r), OnStep: run.add}
	go func() {
		// Detached from the request: the operator's browser is about to be redirected,
		// and a cancelled context here would abandon a request that already went out.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Minute)
		defer cancel()
		res, err := p.Run(ctx, opts)
		if err != nil {
			s.Log.Warn("probe failed", "url", target, "err", err)
		}
		run.finish(res, err)
	}()
	http.Redirect(w, r, link+"&run="+strconv.FormatInt(run.ID, 10), http.StatusSeeOther)
}
