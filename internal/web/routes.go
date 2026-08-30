package web

import "net/http"

// routesSPA registers every page the React SPA owns.
//
// Dashboard is served from the embedded React build, backed by
// GET /api/dashboard (internal/api/dashboard.go). Trace, Probe and Probe
// history are backed by GET/POST /api/trace, POST /api/trace/probe,
// GET /api/trace/run/{id}, GET /api/trace/probe/{id} and
// GET /api/trace/history (internal/api/{trace,probe,probelive}.go). Rule
// lookup and Config search are backed by GET /api/rules and GET /api/search
// (internal/api/{rules,search}.go).
//
// Sites is backed by GET /api/sites and GET /api/sites/{name}
// (internal/api/sites.go). Nodes is backed by GET /api/nodes and
// GET /api/nodes/{id}[/{tab}] (internal/api/nodes.go); Import inventory by
// POST /api/nodes/import (internal/api/nodes.go). Clusters is backed by
// GET /api/clusters and POST /api/clusters/rename (internal/api/clusters.go).
//
// Drift, Certificates and Snapshots are backed by
// GET/POST /api/{drift,certificates,snapshots}...
// (internal/api/{drift,certificates,snapshots}.go). The file viewer is the
// other end of every provenance link in the product, so it is reachable by
// a viewer and lives under the snapshot that holds it.
//
// Settings is a page per section rather than tabs on one, because Users and
// the Audit log are two very different queries and a single screen holding
// both would run both on every visit. Host key approval, including the bulk
// "approve all matching filter" action, is POST /api/hostkeys/{id}/decide —
// no top-level route for it. The diagnostics bundle stays a server-rendered
// download: a plain-text attachment has no JSON shape worth a proto message.
func (s *Server) routesSPA(m *http.ServeMux) {
	m.HandleFunc("GET /{$}", s.auth(s.serveSPA))
	m.HandleFunc("GET /trace", s.auth(s.serveSPA))
	m.HandleFunc("GET /trace/probe", s.auth(s.serveSPA))
	m.HandleFunc("GET /trace/history", s.auth(s.serveSPA))
	m.HandleFunc("GET /rules", s.auth(s.serveSPA))
	m.HandleFunc("GET /search", s.auth(s.serveSPA))

	m.HandleFunc("GET /sites", s.auth(s.serveSPA))
	m.HandleFunc("GET /sites/{name}", s.auth(s.serveSPA))
	m.HandleFunc("GET /nodes", s.auth(s.serveSPA))
	m.HandleFunc("GET /nodes/import", s.auth(s.serveSPA))
	m.HandleFunc("GET /nodes/{id}", s.auth(s.serveSPA))
	m.HandleFunc("GET /nodes/{id}/{tab}", s.auth(s.serveSPA))
	m.HandleFunc("GET /clusters", s.auth(s.serveSPA))

	m.HandleFunc("GET /drift", s.auth(s.serveSPA))
	m.HandleFunc("GET /drift/review/{instanceID}", s.auth(s.serveSPA))
	m.HandleFunc("GET /certificates", s.auth(s.serveSPA))
	m.HandleFunc("GET /certificates/{id}", s.auth(s.serveSPA))
	m.HandleFunc("GET /snapshots", s.auth(s.serveSPA))
	m.HandleFunc("GET /snapshots/{id}/file/{fileID}", s.auth(s.serveSPA))

	m.HandleFunc("GET /collections", s.auth(s.serveSPA))
	m.HandleFunc("GET /settings", s.auth(s.serveSPA))
	m.HandleFunc("GET /settings/{section}", s.auth(s.serveSPA))
	m.HandleFunc("GET /settings/system/bundle", s.admin(s.diagnosticsBundle))
}
