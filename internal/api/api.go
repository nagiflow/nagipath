// Package api is the JSON surface the React SPA talks to (docs/adr/0017).
// Unlike internal/web, which renders HTML and carries the operator UI's
// business logic in its template FuncMap, this package returns plain JSON
// and every field on a response is meant to be display-ready — computed
// here, not recomputed in TypeScript. It is mounted into internal/web's
// mux at /api/ui/, so panic recovery and security headers are inherited
// from internal/web.Server.ServeHTTP; this package adds neither.
package api

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/nagiflow/nagipath/internal/collect"
	"github.com/nagiflow/nagipath/internal/license"
	"github.com/nagiflow/nagipath/internal/store"
)

type Server struct {
	DB *store.DB
	// LicenseStatus is internal/web.Server.LicenseStatus, injected by the
	// caller: license state (and the mutex guarding it) stays owned by
	// internal/web until Phase 4 ports the license admin page here too, at
	// which point this indirection is deleted along with its owner.
	LicenseStatus func(ctx context.Context) (license.Status, string)
	// DemoMode mirrors internal/web.Server.DemoMode, for the header's DEMO
	// badge — a public trial instance refuses every Probe outright, and an
	// operator needs to know before clicking one.
	DemoMode bool
	// Collector and Log are set directly by internal/web after New (same
	// pattern web.go's own doc comment uses for StartedAt/ListenAddr/
	// TLSEnabled: New's parameter list is long enough already).
	Collector *collect.Collector
	Log       *slog.Logger

	mux *http.ServeMux
}

func New(db *store.DB, licenseStatus func(context.Context) (license.Status, string), demoMode bool) *Server {
	s := &Server{DB: db, LicenseStatus: licenseStatus, DemoMode: demoMode}
	m := http.NewServeMux()
	m.HandleFunc("GET /session", s.requireAuth(s.getSession))
	m.HandleFunc("GET /dashboard", s.requireAuth(s.getDashboard))
	m.HandleFunc("GET /clusters", s.requireAuth(s.getClusters))
	m.HandleFunc("POST /clusters/rename", s.requireAdmin(s.postRenameCluster))
	m.HandleFunc("GET /sites", s.requireAuth(s.getSites))
	m.HandleFunc("GET /sites/{name}", s.requireAuth(s.getSite))
	m.HandleFunc("GET /nodes", s.requireAuth(s.getNodes))
	m.HandleFunc("POST /nodes", s.requireAdmin(s.postAddNode))
	m.HandleFunc("GET /nodes/{id}", s.requireAuth(s.getNode))
	m.HandleFunc("GET /nodes/{id}/{tab}", s.requireAuth(s.getNode))
	m.HandleFunc("POST /nodes/{id}/collect", s.requireAdmin(s.postCollectNode))
	m.HandleFunc("POST /nodes/{id}/delete", s.requireAdmin(s.postDeleteNode))
	m.HandleFunc("POST /nodes/{id}/credential", s.requireAdmin(s.postChangeNodeCredential))
	m.HandleFunc("POST /hostkeys/{id}/decide", s.requireAdmin(s.postDecideHostKey))
	m.HandleFunc("GET /drift", s.requireAuth(s.getDrift))
	m.HandleFunc("GET /drift/review/{instanceID}", s.requireAuth(s.getDriftReview))
	m.HandleFunc("POST /drift/recompute", s.requireAdmin(s.postDriftRecompute))
	m.HandleFunc("POST /drift/ignore", s.requireAdmin(s.postDriftIgnore))
	m.HandleFunc("POST /drift/ignore/{id}/delete", s.requireAdmin(s.postDriftUnignore))
	m.HandleFunc("POST /drift/golden", s.requireAdmin(s.postDriftGolden))
	m.HandleFunc("GET /certificates", s.requireAuth(s.getCertificates))
	m.HandleFunc("GET /certificates/{id}", s.requireAuth(s.getCertificate))
	m.HandleFunc("GET /snapshots", s.requireAuth(s.getSnapshots))
	m.HandleFunc("GET /snapshots/{id}/file/{fileID}", s.requireAuth(s.getSnapshotFile))
	s.mux = m
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}
