package api

import (
	"net/http"
	"strconv"
	"strings"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
	"github.com/nagiflow/nagipath/internal/probe"
	"github.com/nagiflow/nagipath/internal/store"
)

// getProbeHistory and getProbeDetail moved to probeservice.go as
// ProbeService's GetProbeHistory and GetProbeDetail RPCs (docs/adr/0018,
// proto/nagipath/api/v1/probe.proto). toPBProbePast, toPBProbeHopEvidence
// and probesPageSize stay here: shared with probeservice.go and
// getProbeHistoryCSV.

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

func toPBProbeHopEvidence(h store.ProbeHopEvidence) *pb.ProbeHopEvidencePB {
	return &pb.ProbeHopEvidencePB{HopOrdinal: int32(h.HopOrdinal), NodeName: h.NodeName, Vendor: h.Vendor,
		Evidence: h.Evidence, Before: h.Before, After: h.After, HasGap: h.HasGap,
		GapVendor: h.GapVendor, GapNote: h.GapNote, GapDirective: h.GapDirective}
}

// getProbeHistoryCSV: GET /trace/history?export=csv is a formatted download,
// not RPC-shaped data (gateway.go's gatewayOrCSV, wired in api.go). The
// export is the filter, not the page: an operator who narrowed to one actor
// and pressed Export means every probe that actor sent.
func (s *Server) getProbeHistoryCSV(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	days := 7
	switch q.Get("range") {
	case "30":
		days = 30
	case "90":
		days = 90
	case "all":
		days = 0
	}
	outcome := q.Get("outcome")
	switch outcome {
	case "completed", "failed", "blocked", "running":
	default:
		outcome = ""
	}

	rows, err := probe.List(ctx, s.DB, probe.Filter{
		URL: q.Get("url"), Q: strings.TrimSpace(q.Get("q")), Actor: q.Get("actor"), Outcome: outcome, Days: days,
		Limit: 5000,
	})
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
		return
	}

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
}
