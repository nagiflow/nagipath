package web

import (
	"net/http"
	"strconv"

	"github.com/nagiflow/nagipath/internal/store"
)

type auditData struct {
	Result    store.AuditResult
	Actions   []string
	Actor     string
	Action    string
	DateRange string
	Empty     bool
}

func (s *Server) audit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	filter := store.AuditFilter{
		Actor:      q.Get("actor"),
		Action:     q.Get("action"),
		DateRange:  q.Get("range"),
		Page:       1,
		PerPage:    50,
		ExportMode: q.Get("export") == "csv",
	}
	if filter.DateRange == "" {
		filter.DateRange = "7d"
	}
	if pageStr := q.Get("page"); pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			filter.Page = p
		}
	}
	if perPageStr := q.Get("per_page"); perPageStr != "" {
		if pp, err := strconv.Atoi(perPageStr); err == nil && pp > 0 && pp <= 200 {
			filter.PerPage = pp
		}
	}

	result, err := s.DB.AuditEventsFiltered(ctx, filter)
	if err != nil {
		s.serverError(w, r, err)
		return
	}

	if filter.ExportMode {
		rows := [][]string{{"at", "actor", "action", "target_kind", "target", "outcome", "detail", "source_ip"}}
		for _, event := range result.Events {
			rows = append(rows, []string{event.At, event.ActorLabel, event.Action,
				event.TargetKind, event.TargetLabel, event.Outcome, event.Detail, event.SourceIP})
		}
		s.writeCSV(w, r, "audit", rows)
		return
	}

	actions, err := s.DB.AuditActions(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}

	data := auditData{
		Result:    result,
		Actions:   actions,
		Actor:     filter.Actor,
		Action:    filter.Action,
		DateRange: filter.DateRange,
		Empty:     result.Total == 0 && filter.Actor == "" && filter.Action == "" && filter.DateRange == "7d",
	}
	s.render(w, r, "audit.html", "Audit log", data)
}
