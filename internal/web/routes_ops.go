package web

import "net/http"

// Collections and Settings.
//
// Settings is a page per section rather than tabs on one, because Users and the
// Audit log are two very different queries and a single screen holding both
// would run both on every visit. The sub-navigation is settingsNav in nav.go.
func (s *Server) routesOps(m *http.ServeMux) {
	// Host key approval is the gate on running any command on a Node, so it is
	// its own action with its own audit entry, reachable from Settings. The
	// bulk-approve form posts here directly (a plain HTML form, CSRF field and
	// all) from HostKeysPage rather than through /api/ui: it lives in
	// inventory_import.go, which this migration does not touch.
	m.HandleFunc("POST /hostkeys/approve", s.admin(s.approveHostKeys))

	// Settings, Collections and every other page in this group are the React
	// SPA now (docs/adr/0017, Phase 4); business logic lives in internal/api.
	m.HandleFunc("GET /collections", s.auth(s.serveSPA))
	m.HandleFunc("GET /settings", s.auth(s.serveSPA))
	m.HandleFunc("GET /settings/{section}", s.auth(s.serveSPA))

	// Diagnostics bundle stays a server-rendered download: a plain-text
	// attachment has no JSON shape worth a proto message (see CSV exports).
	m.HandleFunc("GET /settings/system/bundle", s.admin(s.diagnosticsBundle))

	// Where these lived before Settings gathered them up.
	m.HandleFunc("GET /credentials", moved("/settings/credentials"))
	m.HandleFunc("GET /users", moved("/settings/users"))
	m.HandleFunc("GET /audit", moved("/settings/audit"))
	m.HandleFunc("GET /license", moved("/settings/license"))
	m.HandleFunc("GET /diagnostics", moved("/settings/system"))
	m.HandleFunc("GET /diagnostics/bundle", moved("/settings/system/bundle"))
}
