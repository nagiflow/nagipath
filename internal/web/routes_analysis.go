package web

import "net/http"

// The Analysis group: Drift, Certificates and Snapshots is the SPA now
// (docs/adr/0017): GET/POST /api/ui/{drift,certificates,snapshots}...
// (internal/api/drift.go, certificates.go, snapshots.go).
func (s *Server) routesAnalysis(m *http.ServeMux) {
	m.HandleFunc("GET /drift", s.auth(s.serveSPA))
	m.HandleFunc("GET /drift/review/{instanceID}", s.auth(s.serveSPA))

	m.HandleFunc("GET /certificates", s.auth(s.serveSPA))
	m.HandleFunc("GET /certificates/{id}", s.auth(s.serveSPA))

	m.HandleFunc("GET /snapshots", s.auth(s.serveSPA))
	// The file viewer is the other end of every provenance link in the product,
	// so it is reachable by a viewer and lives under the snapshot that holds it.
	m.HandleFunc("GET /snapshots/{id}/file/{fileID}", s.auth(s.serveSPA))
}
