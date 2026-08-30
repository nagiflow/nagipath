package api

import (
	"context"
	"strings"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
	"github.com/nagiflow/nagipath/internal/probe"
	"github.com/nagiflow/nagipath/internal/store"
	"github.com/nagiflow/nagipath/internal/trace"
)

// getTrace, postTrace, postStartProbe and getProbeRun moved to
// traceservice.go as TraceService's GetTrace/PostTrace/StartProbe/
// GetProbeRun RPCs (docs/adr/0018, proto/nagipath/api/v1/trace.proto).
// traceProv and traceCore stay here: shared with traceservice.go.

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

// traceCore is GetTrace and PostTrace's shared computation: everything
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
			cand.InstId, cand.NodeId = c.Inst.ID, c.Inst.NodeID
			cand.InstDisplayName, cand.InstNodeAddress = c.Inst.DisplayName, c.Inst.NodeAddress
		}
		if c.Listener != nil {
			cand.ListenerPort = int32(c.Listener.Port)
		}
		resp.EntryCandidates = append(resp.EntryCandidates, cand)
	}

	return resp, nil
}
