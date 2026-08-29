package api

import (
	"net/http"
	"strconv"
	"strings"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
	"github.com/nagiflow/nagipath/internal/probe"
	"github.com/nagiflow/nagipath/internal/store"
)

const probesPageSize = 50

func toPBProbePast(p *probe.Past) *pb.ProbePastPB {
	if p == nil {
		return nil
	}
	out := &pb.ProbePastPB{ProbeId: p.ProbeID, Token: p.Token, Method: p.Method, Url: p.URL,
		Status: int32(p.Status), StatusText: p.StatusText, DurationMs: p.DurationMS, Error: p.Err,
		RequestedAt: p.RequestedAt, ActorLabel: p.ActorLabel, OriginHost: p.OriginHost, Outcome: p.Outcome,
		RedirectCount: int32(len(p.Redirects)), LogReads: p.LogReads(), ChangeCount: int32(p.ChangeCount())}
	for _, e := range p.Evidence {
		out.Evidence = append(out.Evidence, &pb.ProbeEvidenceRowPB{HopOrdinal: int32(e.HopOrdinal),
			Instance: e.Instance, Node: e.Node, Kind: e.Kind, LogPath: e.LogPath, Raw: e.Raw,
			Grants: e.Grants, ObservedAt: e.ObservedAt, Prior: e.Prior})
	}
	for _, c := range p.Changes() {
		out.Changes = append(out.Changes, &pb.HopChangePB{Host: c.Host, Prior: c.Prior, Grants: c.Grants,
			Kind: c.Kind, LogPath: c.LogPath, To: c.To()})
	}
	return out
}

// getProbeHistory ports internal/web/trace.go's probeHistory(): the audit
// record for every request nagipath sent on an operator's behalf. Reached
// from one trace it lists that entry point; reached with no url it lists the
// whole installation, which is the same screen and the same row shape.
func (s *Server) getProbeHistory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	resp := &pb.ProbeHistoryResponse{Url: q.Get("url"), Query: strings.TrimSpace(q.Get("q")),
		Range: q.Get("range"), Actor: q.Get("actor"), Outcome: q.Get("outcome")}

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
		apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
		return
	}
	resp.Total = int32(len(rows))
	seen := map[string]bool{}
	for _, p := range rows {
		if p.ActorLabel != "" && !seen[p.ActorLabel] {
			seen[p.ActorLabel] = true
			resp.ActorCount++
		}
	}

	// The export is the filter, not the page: an operator who narrowed to one
	// actor and pressed Export audit means every probe that actor sent.
	if q.Get("export") == "csv" {
		out := [][]string{{"requested_at", "probe_id", "method", "entry_point", "status",
			"outcome", "error", "actor", "source", "evidence_rows", "state_changes"}}
		for _, p := range rows {
			status := ""
			if p.Status > 0 {
				status = strconv.Itoa(p.Status)
			}
			out = append(out, []string{p.RequestedAt, p.Token, p.Method, p.URL, status,
				p.Result, p.Err, p.ActorLabel, p.OriginHost, strconv.Itoa(p.Evidence),
				strings.Join(p.Changes, "; ")})
		}
		s.writeCSV(w, r, "probes", out)
		return
	}

	cursor, _ := strconv.Atoi(q.Get("cursor"))
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
	if id, err := strconv.ParseInt(q.Get("probe"), 10, 64); err == nil && id > 0 {
		sel, _ := probe.Load(ctx, s.DB, id)
		resp.Selected = toPBProbePast(sel)
	} else if len(page) > 0 {
		sel, _ := probe.Load(ctx, s.DB, page[0].ProbeID)
		resp.Selected = toPBProbePast(sel)
	}
	writeProto(w, http.StatusOK, resp)
}

// getProbeDetail ports internal/web/trace.go's probeScreen(): one stored
// Probe's full detail — request, audit, response stats, hop evidence, and
// matched log lines.
func (s *Server) getProbeDetail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		apiError(w, http.StatusBadRequest, "invalid_id", "Invalid probe id.")
		return
	}
	p, err := s.DB.LoadProbeView(ctx, id)
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
		return
	}
	if p == nil {
		apiError(w, http.StatusNotFound, "not_found", "Probe not found.")
		return
	}
	resp := &pb.ProbeDetailResponse{Probe: &pb.ProbeViewPB{ProbeId: p.ProbeID, TraceId: p.TraceID.Int64,
		ActorUsername: p.ActorUsername, Method: p.Method, Url: p.URL, MaxRedirects: int32(p.MaxRedirects),
		CorrelationToken: p.CorrelationToken, OriginHost: p.OriginHost, RequestedAt: p.RequestedAt,
		Status: int32(p.Status), DurationMs: p.DurationMS, RedirectCount: int32(p.RedirectCount),
		ServerHeader: p.ServerHeader, ViaHeader: p.ViaHeader, Result: p.Result,
		LogCapableCount: int32(p.LogCapableCount), VerifiedCount: int32(p.VerifiedCount),
		StillInferred: int32(p.StillInferred), RetentionDays: int32(p.RetentionDays)}}

	hops, err := s.DB.ProbeHopEvidenceRows(ctx, id)
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
		return
	}
	for _, h := range hops {
		resp.Hops = append(resp.Hops, toPBProbeHopEvidence(h))
		if (h.Before == "inferred" || h.Before == "degraded") && (h.After == "verified" || h.After == "observed_effect") {
			resp.StateChanges++
		}
	}

	lines, err := s.DB.ProbeLogLines(ctx, id)
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
		return
	}
	for _, l := range lines {
		resp.LogLines = append(resp.LogLines, &pb.ProbeLogLinePB{NodeName: l.NodeName, LogPath: l.LogPath, RawLine: l.RawLine})
	}

	gaps, err := s.DB.ProbeLogFormatGaps(ctx, id, p.TraceID.Int64)
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
		return
	}
	for _, g := range gaps {
		resp.Gaps = append(resp.Gaps, toPBProbeHopEvidence(g))
	}
	writeProto(w, http.StatusOK, resp)
}

func toPBProbeHopEvidence(h store.ProbeHopEvidence) *pb.ProbeHopEvidencePB {
	return &pb.ProbeHopEvidencePB{HopOrdinal: int32(h.HopOrdinal), NodeName: h.NodeName, Vendor: h.Vendor,
		Evidence: h.Evidence, Before: h.Before, After: h.After, HasGap: h.HasGap,
		GapVendor: h.GapVendor, GapNote: h.GapNote, GapDirective: h.GapDirective}
}
