package web

import "net/http"

// The Explore group: Dashboard, Trace, Rule lookup and Config search — the four
// screens that answer a question rather than list an inventory.
func (s *Server) routesExplore(m *http.ServeMux) {
	// Dashboard is the SPA now (docs/adr/0017): served from the embedded React
	// build, backed by GET /api/ui/dashboard (internal/api/dashboard.go).
	m.HandleFunc("GET /{$}", s.auth(s.serveSPA))

	// Trace, Probe and Probe history are the SPA now (docs/adr/0017, Phase 6):
	// GET/POST /api/ui/trace, POST /api/ui/trace/probe, GET /api/ui/trace/run/{id},
	// GET /api/ui/trace/probe/{id} and GET /api/ui/trace/history
	// (internal/api/{trace,probe,probelive}.go).
	m.HandleFunc("GET /trace", s.auth(s.serveSPA))
	m.HandleFunc("GET /trace/probe", s.auth(s.serveSPA))
	m.HandleFunc("GET /trace/history", s.auth(s.serveSPA))

	// Rule lookup and Config search are the SPA now (docs/adr/0017, Phase 5):
	// GET /api/ui/rules and GET /api/ui/search (internal/api/{rules,search}.go).
	m.HandleFunc("GET /rules", s.auth(s.serveSPA))
	m.HandleFunc("GET /search", s.auth(s.serveSPA))
}
