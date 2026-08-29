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
	"net/http"

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

	mux *http.ServeMux
}

func New(db *store.DB, licenseStatus func(context.Context) (license.Status, string), demoMode bool) *Server {
	s := &Server{DB: db, LicenseStatus: licenseStatus, DemoMode: demoMode}
	m := http.NewServeMux()
	m.HandleFunc("GET /session", s.requireAuth(s.getSession))
	m.HandleFunc("GET /dashboard", s.requireAuth(s.getDashboard))
	m.HandleFunc("GET /clusters", s.requireAuth(s.getClusters))
	m.HandleFunc("POST /clusters/rename", s.requireAdmin(s.postRenameCluster))
	s.mux = m
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}
