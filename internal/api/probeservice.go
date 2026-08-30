package api

import (
	"context"
	"strconv"
	"strings"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
	"github.com/nagiflow/nagipath/internal/probe"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// probeService implements pb.ProbeServiceServer (proto/nagipath/api/v1/probe.proto).
// Ports getProbeHistory/getProbeDetail (formerly probe.go); toPBProbePast
// and toPBProbeHopEvidence stay in probe.go, shared with getProbeHistoryCSV.
type probeService struct {
	pb.UnimplementedProbeServiceServer
	s *Server
}

func (c *probeService) GetProbeHistory(ctx context.Context, req *pb.GetProbeHistoryRequest) (*pb.ProbeHistoryResponse, error) {
	s := c.s
	resp := &pb.ProbeHistoryResponse{Url: req.Url, Query: strings.TrimSpace(req.Q),
		Range: req.Range, Actor: req.Actor, Outcome: req.Outcome}

	days := 7
	switch resp.Range {
	case "30":
		days = 30
	case "90":
		days = 90
	case "all":
		days = 0
	default:
		resp.Range = "7"
	}
	switch resp.Outcome {
	case "completed", "failed", "blocked", "running":
	default:
		resp.Outcome = ""
	}
	resp.Actors, _ = probe.Actors(ctx, s.DB)

	rows, err := probe.List(ctx, s.DB, probe.Filter{
		URL: resp.Url, Q: resp.Query, Actor: resp.Actor, Outcome: resp.Outcome, Days: days,
		// ponytail: one window's worth in memory, filtered and paged in Go, exactly
		// like the snapshots list. A probe is rate-limited per operator, so this is
		// thousands of rows at the very worst.
		Limit: 5000,
	})
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	resp.Total = int32(len(rows))
	seen := map[string]bool{}
	for _, p := range rows {
		if p.ActorLabel != "" && !seen[p.ActorLabel] {
			seen[p.ActorLabel] = true
			resp.ActorCount++
		}
	}

	cursor, _ := strconv.Atoi(req.Cursor)
	if cursor < 0 || cursor > len(rows) {
		cursor = 0
	}
	end := min(cursor+probesPageSize, len(rows))
	if end < len(rows) {
		resp.HasMore = true
		resp.NextCursor = strconv.Itoa(end)
	}
	page := rows[cursor:end]
	if len(page) > 0 {
		resp.From, resp.To = int32(cursor+1), int32(end)
	}
	for _, p := range page {
		resp.Probes = append(resp.Probes, &pb.ProbeRecordPB{ProbeId: p.ProbeID, Token: p.Token,
			Method: p.Method, Url: p.URL, Status: int32(p.Status), Result: p.Result, Error: p.Err,
			ActorLabel: p.ActorLabel, OriginHost: p.OriginHost, RequestedAt: p.RequestedAt,
			Evidence: int32(p.Evidence), Verified: int32(p.Verified), Changes: p.Changes, Raised: int32(p.Raised)})
	}

	// A row selects itself by probe id. The default is the newest row the
	// filter matched, so the detail panel is never empty next to a table
	// that has rows.
	if req.Probe > 0 {
		sel, _ := probe.Load(ctx, s.DB, req.Probe)
		resp.Selected = toPBProbePast(sel)
	} else if len(page) > 0 {
		sel, _ := probe.Load(ctx, s.DB, page[0].ProbeID)
		resp.Selected = toPBProbePast(sel)
	}
	return resp, nil
}

func (c *probeService) GetProbeDetail(ctx context.Context, req *pb.GetProbeDetailRequest) (*pb.ProbeDetailResponse, error) {
	if err := requireAdminRPC(ctx); err != nil {
		return nil, err
	}
	s := c.s
	p, err := s.DB.LoadProbeView(ctx, req.Id)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	if p == nil {
		return nil, status.Error(codes.NotFound, "Probe not found.")
	}
	resp := &pb.ProbeDetailResponse{Probe: &pb.ProbeViewPB{ProbeId: p.ProbeID, TraceId: p.TraceID.Int64,
		ActorUsername: p.ActorUsername, Method: p.Method, Url: p.URL, MaxRedirects: int32(p.MaxRedirects),
		CorrelationToken: p.CorrelationToken, OriginHost: p.OriginHost, RequestedAt: p.RequestedAt,
		Status: int32(p.Status), DurationMs: p.DurationMS, RedirectCount: int32(p.RedirectCount),
		ServerHeader: p.ServerHeader, ViaHeader: p.ViaHeader, Result: p.Result,
		LogCapableCount: int32(p.LogCapableCount), VerifiedCount: int32(p.VerifiedCount),
		StillInferred: int32(p.StillInferred), RetentionDays: int32(p.RetentionDays)}}

	hops, err := s.DB.ProbeHopEvidenceRows(ctx, req.Id)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	for _, h := range hops {
		resp.Hops = append(resp.Hops, toPBProbeHopEvidence(h))
		if (h.Before == "inferred" || h.Before == "degraded") && (h.After == "verified" || h.After == "observed_effect") {
			resp.StateChanges++
		}
	}

	lines, err := s.DB.ProbeLogLines(ctx, req.Id)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	for _, l := range lines {
		resp.LogLines = append(resp.LogLines, &pb.ProbeLogLinePB{NodeName: l.NodeName, LogPath: l.LogPath, RawLine: l.RawLine})
	}

	gaps, err := s.DB.ProbeLogFormatGaps(ctx, req.Id, p.TraceID.Int64)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	for _, g := range gaps {
		resp.Gaps = append(resp.Gaps, toPBProbeHopEvidence(g))
	}
	return resp, nil
}
