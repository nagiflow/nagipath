package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
	"github.com/nagiflow/nagipath/internal/probe"
	"github.com/nagiflow/nagipath/internal/store"
	"github.com/nagiflow/nagipath/internal/trace"
)

// traceProv ports internal/web/trace.go's traceProv: the Provenance panel
// for one rule of a trace — the configuration lines it was parsed from, or
// why there are none to show. The default is the first rule that decided
// anything, so the panel is never empty on a trace that found a route.
func (s *Server) traceProv(ctx context.Context, t *trace.Trace, want int64) *pb.ProvenancePB {
	var hop *trace.Hop
	var rule *trace.Rule
	for _, h := range t.Hops {
		for _, hr := range fired(h.Rules) {
			if hr.Rule == nil {
				continue
			}
			if (want == 0 && rule == nil) || (want != 0 && hr.Rule.ID == want) {
				hop, rule = h, hr.Rule
			}
		}
	}
	if rule == nil {
		return nil
	}
	v := &pb.ProvenancePB{RuleId: rule.ID, Rule: strings.TrimSpace(rule.Directive + " " + rule.Args),
		Path: rule.Path, SnapshotId: rule.SnapshotID, FileId: rule.FileID, ByteStart: int32(rule.ByteStart)}
	if hop.Inst != nil {
		v.Instance = instLabel(hop.Inst)
	}
	switch {
	case hop.Route != nil:
		v.Object = routeKind(hop.Route.MatchType) + " " + hop.Route.Pattern
	case hop.Site != nil:
		v.Object = siteKind(hop.Site.Kind) + " " + hop.Site.PrimaryName
	}
	if rule.FileID == 0 || rule.SnapshotID == 0 {
		v.Missing = "this rule was parsed before nagipath recorded file provenance, so there is no excerpt to show"
		return v
	}
	files, err := s.DB.SnapshotFiles(ctx, rule.SnapshotID)
	if err != nil {
		v.Missing = "the snapshot this rule came from could not be read back"
		return v
	}
	var ref *store.FileRef
	for i := range files {
		if files[i].ID == rule.FileID {
			ref = &files[i]
		}
	}
	if ref == nil {
		v.Missing = "the file this rule came from is no longer in that snapshot"
		return v
	}
	v.Digest, v.Path = ref.Digest, ref.Path
	body, err := s.DB.Blob(ctx, ref.Digest)
	if err != nil {
		v.Missing = "the stored copy of " + ref.Path + " could not be read"
		return v
	}
	lines := strings.Split(string(body), "\n")
	at := strings.Count(string(body[:min(rule.ByteStart, len(body))]), "\n")
	v.Line = int32(at + 1)
	for i := max(0, at-3); i < min(len(lines), at+5); i++ {
		v.Lines = append(v.Lines, &pb.ProvenanceLine{N: int32(i + 1), Text: lines[i], On: i == at})
	}
	return v
}

// getTrace ports internal/web/trace.go's trace() for the read-only GET: the
// same site and route selection Rule lookup uses, followed hop by hop, plus
// whatever Probe evidence exists for the same URL. It never stores anything
// — see postTrace for the mutation that does.
func (s *Server) getTrace(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	method := "GET"
	if m := strings.ToUpper(strings.TrimSpace(q.Get("method"))); m == "HEAD" {
		method = m
	}
	tq, err := parseTarget(q.Get("url"))
	if err != nil {
		apiError(w, http.StatusUnprocessableEntity, "invalid_url", err.Error())
		return
	}
	if tq.Hostname == "" {
		tq = trace.Query{Scheme: q.Get("scheme"), Hostname: strings.TrimSpace(q.Get("hostname")),
			Path: strings.TrimSpace(q.Get("path"))}
		if p, err := strconv.Atoi(q.Get("port")); err == nil {
			tq.Port = p
		}
	}
	runID, _ := strconv.ParseInt(q.Get("run"), 10, 64)
	provID, _ := strconv.ParseInt(q.Get("prov"), 10, 64)
	resp, err := s.traceCore(r.Context(), tq, method, runID, provID)
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
		return
	}
	writeProto(w, http.StatusOK, resp)
}

// postTrace ports internal/web/trace.go's trace() for the POST: the same
// computation as getTrace, but it also stores the Trace and records an
// audit entry — a Probe attaches its evidence to a stored Trace's hop rows,
// so there has to be one before a Probe can run against it.
func (s *Server) postTrace(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL      string `json:"url"`
		Scheme   string `json:"scheme"`
		Hostname string `json:"hostname"`
		Path     string `json:"path"`
		Port     int    `json:"port"`
		Method   string `json:"method"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apiError(w, http.StatusBadRequest, "invalid_body", "Could not parse request body.")
		return
	}
	method := "GET"
	if strings.ToUpper(strings.TrimSpace(body.Method)) == "HEAD" {
		method = "HEAD"
	}
	tq, err := parseTarget(body.URL)
	if err != nil {
		apiError(w, http.StatusUnprocessableEntity, "invalid_url", err.Error())
		return
	}
	if tq.Hostname == "" {
		tq = trace.Query{Scheme: body.Scheme, Hostname: strings.TrimSpace(body.Hostname), Path: strings.TrimSpace(body.Path), Port: body.Port}
	}

	ctx := r.Context()
	resp, err := s.traceCore(ctx, tq, method, 0, 0)
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
		return
	}
	if resp.Asked {
		// Only a POST stores the Trace. A shared link that recomputes on every
		// view would otherwise fill the table with duplicates.
		u := userOf(r)
		top, err := trace.Load(ctx, s.DB)
		if err == nil {
			result := trace.Walk(top, tq)
			if id, err := trace.Save(ctx, s.DB, result, nil); err != nil {
				s.Log.Warn("trace not stored", "err", err)
			} else {
				resp.TraceId = id
			}
		}
		_ = s.DB.Audit(ctx, &u.ID, "trace.compute", "trace", nil, tq.Scheme+"://"+tq.Hostname+tq.Path)
	}
	writeProto(w, http.StatusOK, resp)
}

// traceCore is getTrace and postTrace's shared computation: everything
// except whether the query came from URL params or a JSON body, and whether
// the result gets stored.
func (s *Server) traceCore(ctx context.Context, tq trace.Query, method string, runID, provID int64) (*pb.TraceResponse, error) {
	resp := &pb.TraceResponse{Method: method}
	if tq.Hostname == "" {
		instances, _ := s.DB.Instances(ctx)
		resp.Empty = len(instances) == 0
		recent, _ := trace.Recent(ctx, s.DB, 20)
		for _, t := range recent {
			resp.Recent = append(resp.Recent, &pb.TraceRecent{Id: t.ID, Scheme: t.Scheme, Hostname: t.Hostname,
				Path: t.Path, Port: int32(t.Port), ComputedAt: t.ComputedAt, HopCount: int32(t.HopCount),
				Confidence: t.Confidence, TerminalReason: t.TerminalReason})
		}
		return resp, nil
	}

	top, err := trace.Load(ctx, s.DB)
	if err != nil {
		return nil, err
	}
	result := trace.Walk(top, tq)
	resp.Asked = true
	resp.Scheme, resp.Hostname, resp.Path, resp.Port = result.Query.Scheme, result.Query.Hostname, result.Query.Path, int32(result.Query.Port)
	resp.Url = targetURL(result.Query)
	resp.TerminalReason = reasonText(result.TerminalReason)

	last, _ := probe.Last(ctx, s.DB, resp.Url)
	resp.ProbesCount = int32(probe.Count(ctx, s.DB, resp.Url))
	if last != nil {
		resp.LastProbe = &pb.LastProbePB{Outcome: last.Outcome, Method: last.Method,
			RequestedAt: last.RequestedAt, Error: last.Err, Probed: probedHops(result.Hops, last)}
	}

	if runID != 0 {
		if run := s.probeRunView(runID); run != nil {
			resp.Run = &pb.ProbeRunPB{Id: run.ID, Url: run.URL, Method: run.Method, Done: run.Done, Error: run.Err,
				State: map[int32]string{}}
			for _, st := range run.Steps {
				resp.Run.Steps = append(resp.Run.Steps, &pb.ProbeStepPB{Text: st.Text, Ordinal: int32(st.Ordinal), Instance: st.Instance, State: st.State})
			}
			for k, v := range run.State {
				resp.Run.State[int32(k)] = v
			}
		}
	}

	resp.Prov = s.traceProv(ctx, result, provID)
	resp.ProvPicked = provID != 0

	depth := hopDepths(result.Hops)
	labels := hopLabels(result.Hops, depth)
	for i, h := range result.Hops {
		resp.Hops = append(resp.Hops, toPBHop(h, labels[i], depth[i]))
		switch {
		case h.Confidence == "verified":
			resp.VerifiedCount++
		case h.Confidence == "inferred":
			resp.InferredCount++
		case h.Confidence == "degraded":
			resp.DegradedCount++
		case h.IsExternal:
			resp.ExternalCount++
		}
	}
	f, total := firedOf(result.Hops)
	resp.FiredCount, resp.ScopeCount, resp.CollapsedCount = int32(f), int32(total), int32(total-f)

	for _, c := range result.EntryCandidates {
		cand := &pb.TraceCandidate{Selected: c.Selected, Reason: c.Reason}
		if c.Inst != nil {
			cand.InstId, cand.InstDisplayName, cand.InstNodeAddress = c.Inst.ID, c.Inst.DisplayName, c.Inst.NodeAddress
		}
		if c.Listener != nil {
			cand.ListenerPort = int32(c.Listener.Port)
		}
		resp.EntryCandidates = append(resp.EntryCandidates, cand)
	}

	return resp, nil
}

// postStartProbe ports internal/web/probelive.go's startTraceProbe as a JSON
// mutation: it stores the Trace first when the caller did not already have
// one, starts the Probe in a detached goroutine, and returns the run id for
// the client to poll via GET /trace/run/{id}.
func (s *Server) postStartProbe(w http.ResponseWriter, r *http.Request) {
	ctx, u := r.Context(), userOf(r)
	var body struct {
		URL    string `json:"url"`
		Method string `json:"method"`
		Trace  int64  `json:"trace"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apiError(w, http.StatusBadRequest, "invalid_body", "Could not parse request body.")
		return
	}
	tq, err := parseTarget(body.URL)
	if err != nil {
		apiError(w, http.StatusUnprocessableEntity, "invalid_url", err.Error())
		return
	}
	method := strings.ToUpper(strings.TrimSpace(body.Method))
	if method != "HEAD" {
		method = "GET"
	}
	target := targetURL(tq)

	// Both guardrails below refuse before anything is stored or sent: a demo
	// instance or a rate-limited operator gets nothing on the wire, not a
	// request that races a check (probe.md §2). Denied here still gets an
	// audit_event, because a refused Probe is still something an operator
	// asked nagipath to do.
	if s.DemoMode {
		_ = s.DB.AuditDetail(ctx, &u.ID, "probe.run", "probe", nil, target,
			map[string]any{"method": method, "reason": "demo_mode"}, "denied", remoteAddr(r))
		apiError(w, http.StatusForbidden, "demo_mode", "This is a demo instance; NAGIPATH_DEMO_MODE refuses every probe.")
		return
	}
	if limited, err := probe.RateLimited(ctx, s.DB, u.ID, target); err != nil {
		apiError(w, http.StatusInternalServerError, "rate_limit_check_failed", err.Error())
		return
	} else if limited {
		_ = s.DB.AuditDetail(ctx, &u.ID, "probe.run", "probe", nil, target,
			map[string]any{"method": method, "reason": "rate_limited"}, "denied", remoteAddr(r))
		apiError(w, http.StatusTooManyRequests, "rate_limited", fmt.Sprintf(
			"rate limit reached: %d probe(s) already sent to this entry point in the last %ds by this operator",
			s.DB.SettingInt(ctx, "probe_rate_limit_max"), s.DB.SettingInt(ctx, "probe_rate_limit_window_seconds")))
		return
	}

	traceID := body.Trace
	if traceID == 0 {
		top, err := trace.Load(ctx, s.DB)
		if err != nil {
			apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
			return
		}
		id, err := trace.Save(ctx, s.DB, trace.Walk(top, tq), nil)
		if err != nil {
			apiError(w, http.StatusInternalServerError, "save_failed",
				"the trace could not be stored, so a probe has nothing to attach evidence to: "+err.Error())
			return
		}
		traceID = id
	}

	run := s.startProbe(ctx, u.ID, remoteAddr(r), target, method, traceID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "runId": run.ID, "traceId": traceID, "url": target})
}

// getProbeRun is the poll target a running Probe's client re-fetches every
// couple of seconds until Done — see probelive.go's doc comment on why this
// is polling rather than SSE.
func (s *Server) getProbeRun(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		apiError(w, http.StatusBadRequest, "invalid_id", "Invalid run id.")
		return
	}
	run := s.probeRunView(id)
	if run == nil {
		apiError(w, http.StatusNotFound, "not_found", "That probe run is not in memory (the process may have restarted).")
		return
	}
	resp := &pb.ProbeRunPB{Id: run.ID, Url: run.URL, Method: run.Method, Done: run.Done, Error: run.Err, State: map[int32]string{}}
	for _, st := range run.Steps {
		resp.Steps = append(resp.Steps, &pb.ProbeStepPB{Text: st.Text, Ordinal: int32(st.Ordinal), Instance: st.Instance, State: st.State})
	}
	for k, v := range run.State {
		resp.State[int32(k)] = v
	}
	writeProto(w, http.StatusOK, resp)
}
