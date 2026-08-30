package api

import (
	"context"
	"fmt"
	"strings"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
	"github.com/nagiflow/nagipath/internal/probe"
	"github.com/nagiflow/nagipath/internal/trace"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// traceService implements pb.TraceServiceServer (proto/nagipath/api/v1/trace.proto).
// Ports getTrace/postTrace/postStartProbe/getProbeRun (formerly trace.go);
// traceCore, traceProv and probelive.go's startProbe/probeRunView stay
// Server methods — trace_helpers.go's pure functions are unchanged.
type traceService struct {
	pb.UnimplementedTraceServiceServer
	s *Server
}

func targetFrom(url, scheme, hostname, path string, port int32) (trace.Query, error) {
	tq, err := parseTarget(url)
	if err != nil {
		return trace.Query{}, err
	}
	if tq.Hostname == "" {
		tq = trace.Query{Scheme: scheme, Hostname: strings.TrimSpace(hostname), Path: strings.TrimSpace(path), Port: int(port)}
	}
	return tq, nil
}

func (c *traceService) GetTrace(ctx context.Context, req *pb.GetTraceRequest) (*pb.TraceResponse, error) {
	method := "GET"
	if m := strings.ToUpper(strings.TrimSpace(req.Method)); m == "HEAD" {
		method = m
	}
	tq, err := targetFrom(req.Url, req.Scheme, req.Hostname, req.Path, req.Port)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	resp, err := c.s.traceCore(ctx, tq, method, req.Run, req.Prov)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	return resp, nil
}

// PostTrace is the same computation as GetTrace, but it also stores the
// Trace and records an audit entry — a Probe attaches its evidence to a
// stored Trace's hop rows, so there has to be one before a Probe can run
// against it.
func (c *traceService) PostTrace(ctx context.Context, req *pb.PostTraceRequest) (*pb.TraceResponse, error) {
	s := c.s
	method := "GET"
	if strings.ToUpper(strings.TrimSpace(req.Method)) == "HEAD" {
		method = "HEAD"
	}
	tq, err := targetFrom(req.Url, req.Scheme, req.Hostname, req.Path, req.Port)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	resp, err := s.traceCore(ctx, tq, method, 0, 0)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	if resp.Asked {
		// Only a POST stores the Trace. A shared link that recomputes on every
		// view would otherwise fill the table with duplicates.
		u := userOf(ctx)
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
	return resp, nil
}

// StartProbe stores the Trace first when the caller did not already have
// one, starts the Probe in a detached goroutine, and returns the run id for
// the client to poll via GetProbeRun.
func (c *traceService) StartProbe(ctx context.Context, req *pb.StartProbeRequest) (*pb.StartProbeResponse, error) {
	if err := requireAdminRPC(ctx); err != nil {
		return nil, err
	}
	s := c.s
	u := userOf(ctx)
	tq, err := parseTarget(req.Url)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method != "HEAD" {
		method = "GET"
	}
	target := targetURL(tq)
	addr := remoteAddrOf(ctx)

	// Both guardrails below refuse before anything is stored or sent: a demo
	// instance or a rate-limited operator gets nothing on the wire, not a
	// request that races a check (probe.md §2). Denied here still gets an
	// audit_event, because a refused Probe is still something an operator
	// asked nagipath to do.
	if s.DemoMode {
		_ = s.DB.AuditDetail(ctx, &u.ID, "probe.run", "probe", nil, target,
			map[string]any{"method": method, "reason": "demo_mode"}, "denied", addr)
		return nil, status.Error(codes.PermissionDenied, "This is a demo instance; NAGIPATH_DEMO_MODE refuses every probe.")
	}
	if limited, err := probe.RateLimited(ctx, s.DB, u.ID, target); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	} else if limited {
		_ = s.DB.AuditDetail(ctx, &u.ID, "probe.run", "probe", nil, target,
			map[string]any{"method": method, "reason": "rate_limited"}, "denied", addr)
		return nil, status.Error(codes.ResourceExhausted, fmt.Sprintf(
			"rate limit reached: %d probe(s) already sent to this entry point in the last %ds by this operator",
			s.DB.SettingInt(ctx, "probe_rate_limit_max"), s.DB.SettingInt(ctx, "probe_rate_limit_window_seconds")))
	}

	traceID := req.Trace
	if traceID == 0 {
		top, err := trace.Load(ctx, s.DB)
		if err != nil {
			return nil, status.Error(codes.Unavailable, err.Error())
		}
		id, err := trace.Save(ctx, s.DB, trace.Walk(top, tq), nil)
		if err != nil {
			return nil, status.Error(codes.Internal,
				"the trace could not be stored, so a probe has nothing to attach evidence to: "+err.Error())
		}
		traceID = id
	}

	run := s.startProbe(ctx, u.ID, addr, target, method, traceID)
	return &pb.StartProbeResponse{Ok: true, RunId: run.ID, TraceId: traceID, Url: target}, nil
}

// GetProbeRun is the poll target a running Probe's client re-fetches every
// couple of seconds until Done — see probelive.go's doc comment on why this
// is polling rather than SSE.
func (c *traceService) GetProbeRun(ctx context.Context, req *pb.GetProbeRunRequest) (*pb.ProbeRunPB, error) {
	run := c.s.probeRunView(req.Id)
	if run == nil {
		return nil, status.Error(codes.NotFound, "That probe run is not in memory (the process may have restarted).")
	}
	resp := &pb.ProbeRunPB{Id: run.ID, Url: run.URL, Method: run.Method, Done: run.Done, Error: run.Err, State: map[int32]string{}}
	for _, st := range run.Steps {
		resp.Steps = append(resp.Steps, &pb.ProbeStepPB{Text: st.Text, Ordinal: int32(st.Ordinal), Instance: st.Instance, State: st.State})
	}
	for k, v := range run.State {
		resp.State[int32(k)] = v
	}
	return resp, nil
}
