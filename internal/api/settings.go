package api

import (
	"net/http"
	"strconv"

	"github.com/nagiflow/nagipath/internal/store"
)

// Version is stamped by internal/web.Version — set once in api.go's New so
// the diagnostics page (settingsservice.go's GetDiagnostics) can report the
// same build identifier the Probe User-Agent uses, without this package
// importing internal/web (which already imports this one).
var Version = "dev"

// Every settings.go handler moved to settingsservice.go as SettingsService's
// RPCs (docs/adr/0018, proto/nagipath/api/v1/settings.proto). auditFilterFrom
// and getAuditCSV stay here: shared with settingsservice.go's GetAudit, and
// GET /settings/audit?export=csv is a formatted download, not RPC-shaped
// data (gateway.go's gatewayOrCSV, wired in api.go).

func auditFilterFrom(actor, action, dateRange string, page, perPage int) store.AuditFilter {
	filter := store.AuditFilter{Actor: actor, Action: action, DateRange: dateRange, Page: 1, PerPage: 50}
	if filter.DateRange == "" {
		filter.DateRange = "7d"
	}
	if page > 0 {
		filter.Page = page
	}
	if perPage > 0 && perPage <= 200 {
		filter.PerPage = perPage
	}
	return filter
}

func (s *Server) getAuditCSV(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	perPage, _ := strconv.Atoi(q.Get("per_page"))
	filter := auditFilterFrom(q.Get("actor"), q.Get("action"), q.Get("range"), page, perPage)
	filter.ExportMode = true

	result, err := s.DB.AuditEventsFiltered(ctx, filter)
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
		return
	}
	rows := [][]string{{"at", "actor", "action", "target_kind", "target", "outcome", "detail", "source_ip"}}
	for _, e := range result.Events {
		rows = append(rows, []string{e.At, e.ActorLabel, e.Action, e.TargetKind, e.TargetLabel, e.Outcome, e.Detail, e.SourceIP})
	}
	s.writeCSV(w, r, "audit", rows)
}
