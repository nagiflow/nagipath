package api

import (
	"context"
	"sync"
	"time"

	"github.com/nagiflow/nagipath/internal/probe"
)

// A live Probe on the Trace screen. The Trace itself is answered from collected
// configuration; this is the operator asking nagipath to send one real request and
// read the access log on every hop, which is the only thing that can raise a hop to
// Verified. It stays operator-triggered and audited (probe.md §6) — the graph
// following along does not make it a background caller.
//
// ponytail: runs live in memory and is polled by the client every couple of
// seconds — the same mechanism internal/web's htmx page used, just via
// TanStack Query's refetchInterval instead of hx-trigger. A probe is a few
// seconds of progress for one operator, so a restart mid-probe is a re-run,
// not lost data — the evidence itself is already in the database. True SSE
// would be a nicer transport for a boolean/text stream like this, but it is
// not a measurably better one; ADR-0017 makes the same call for the node
// collection running-flag.
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

// runView is the snapshot a poll reads. Copied under the lock so a response
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

// startProbe sends the request and follows the path in a detached goroutine,
// returning the run id immediately for the caller to poll. It stores the
// Trace first when the caller did not already have one: a Probe attaches its
// evidence to hop rows, so there has to be a Trace for it to attach to.
func (s *Server) startProbe(ctx context.Context, actorID int64, originHost, target, method string, traceID int64) *probeRun {
	run := &probeRun{TraceID: traceID, URL: target, Method: method, state: map[int]string{}}
	s.probeMu.Lock()
	s.probeSeq++
	run.ID = s.probeSeq
	if s.probeRuns == nil {
		s.probeRuns = map[int64]*probeRun{}
	}
	s.probeRuns[run.ID] = run
	s.probeMu.Unlock()

	p := &probe.Prober{DB: s.DB, Dialer: s.Collector.Dialer, Log: s.Log, Version: Version}
	opts := probe.Options{TraceID: traceID, URL: target, Method: method,
		MaxRedirects: s.DB.SettingInt(ctx, "probe_max_redirects"),
		ActorID:      actorID, OriginHost: originHost, OnStep: run.add}
	go func() {
		// Detached from the request: the caller's response is about to go out, and a
		// cancelled context here would abandon a request that already went out.
		runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Minute)
		defer cancel()
		res, err := p.Run(runCtx, opts)
		if err != nil {
			s.Log.Warn("probe failed", "url", target, "err", err)
		}
		run.finish(res, err)
	}()
	return run
}
