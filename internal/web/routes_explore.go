package web

import "net/http"

// The Explore group: Dashboard, Trace, Rule lookup and Config search — the four
// screens that answer a question rather than list an inventory.
func (s *Server) routesExplore(m *http.ServeMux) {
	// Dashboard is the SPA now (docs/adr/0017): served from the embedded React
	// build, backed by GET /api/ui/dashboard (internal/api/dashboard.go).
	m.HandleFunc("GET /{$}", s.auth(s.serveSPA))

	// Trace accepts POST as well as GET so a long entry point can come out of a
	// form body; both render the same screen.
	m.HandleFunc("GET /trace", s.auth(s.trace))
	m.HandleFunc("POST /trace", s.auth(s.trace))
	// Probe screen: full detail of one stored probe. Admin-only.
	m.HandleFunc("GET /trace/probe", s.admin(s.probeScreen))
	// A Probe is always operator-initiated and always audited, so it is a POST
	// by an admin and never a side effect of loading a page.
	m.HandleFunc("POST /trace/probe", s.admin(s.startTraceProbe))
	// Probe history for a given URL or all probes
	m.HandleFunc("GET /trace/history", s.auth(s.probeHistory))

	m.HandleFunc("GET /rules", s.auth(s.rules))
	m.HandleFunc("GET /search", s.auth(s.search))
}
