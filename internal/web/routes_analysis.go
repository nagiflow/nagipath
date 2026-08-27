package web

import "net/http"

// The Analysis group: Drift, Certificates and Snapshots — what changed, what is
// about to expire, and the immutable text both are measured against.
func (s *Server) routesAnalysis(m *http.ServeMux) {
	m.HandleFunc("GET /drift", s.auth(s.drift))
	m.HandleFunc("GET /drift/review/{instanceID}", s.auth(s.driftReview))
	m.HandleFunc("POST /drift/recompute", s.admin(s.driftRecompute))
	m.HandleFunc("POST /drift/ignore", s.admin(s.driftIgnore))
	m.HandleFunc("POST /drift/ignore/{id}/delete", s.admin(s.driftUnignore))
	m.HandleFunc("POST /drift/golden", s.admin(s.driftGolden))

	m.HandleFunc("GET /certificates", s.auth(s.certificates))
	m.HandleFunc("GET /certificates/{id}", s.auth(s.certificateDetail))

	m.HandleFunc("GET /snapshots", s.auth(s.snapshots))
	// The file viewer is the other end of every provenance link in the product,
	// so it is reachable by a viewer and lives under the snapshot that holds it.
	m.HandleFunc("GET /snapshots/{id}/file/{fileID}", s.auth(s.snapshotFile))
}
