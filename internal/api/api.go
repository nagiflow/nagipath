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
	"time"

	"github.com/nagiflow/nagipath/internal/collect"
	"github.com/nagiflow/nagipath/internal/keys"
	"github.com/nagiflow/nagipath/internal/license"
	"github.com/nagiflow/nagipath/internal/store"
)

type Server struct {
	DB *store.DB
	// LicenseStatus is internal/web.Server.LicenseStatus, injected by the
	// caller: license state (and the mutex guarding it) stays owned by
	// internal/web, since it is written from more than one place (server
	// startup and this package's postInstallLicense).
	LicenseStatus func(ctx context.Context) (license.Status, string)
	// CurrentLicense and InstallLicense are the other two internal/web
	// license-mutex operations the Settings > License page needs: reading
	// the License currently loaded in memory, and installing a new one
	// (parse, persist to disk, swap the in-memory pointer, record
	// license_state) — all of it stays in internal/web/license.go so the
	// mutex has one owner.
	CurrentLicense func() *license.License
	InstallLicense func(ctx context.Context, raw []byte, by *int64) (*license.License, error)
	// DemoMode mirrors internal/web.Server.DemoMode, for the header's DEMO
	// badge — a public trial instance refuses every Probe outright, and an
	// operator needs to know before clicking one.
	DemoMode bool
	// Master and LicensePath feed the Settings > Master key page; set
	// directly by internal/web after New, same as Collector/Log below.
	Master      *keys.Master
	LicensePath string
	// Diag returns StartedAt/ListenAddr/TLSEnabled for Settings > System. A
	// closure rather than three fields: internal/web sets those three on
	// itself after New returns (see its Server.StartedAt doc comment), so a
	// value copied at New time would stay zero.
	Diag func() (started time.Time, listenAddr string, tlsEnabled bool)
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

	m.HandleFunc("GET /settings/credentials", s.requireAdmin(s.getCredentials))
	m.HandleFunc("POST /settings/credentials", s.requireAdmin(s.postAddCredential))
	m.HandleFunc("GET /settings/hostkeys", s.requireAdmin(s.getHostKeys))
	m.HandleFunc("GET /settings/masterkey", s.requireAdmin(s.getMasterKey))
	m.HandleFunc("GET /settings/collection-defaults", s.requireAdmin(s.getCollectionDefaults))
	m.HandleFunc("POST /settings/collection-defaults", s.requireAdmin(s.postCollectionDefaults))
	m.HandleFunc("GET /settings/retention", s.requireAdmin(s.getRetention))
	m.HandleFunc("POST /settings/retention", s.requireAdmin(s.postRetention))
	m.HandleFunc("POST /settings/retention/prune", s.requireAdmin(s.postRunRetention))
	m.HandleFunc("GET /settings/users", s.requireAdmin(s.getUsers))
	m.HandleFunc("POST /settings/users", s.requireAdmin(s.postAddUser))
	m.HandleFunc("POST /settings/users/{id}/disable", s.requireAdmin(s.postSetUserDisabled(true)))
	m.HandleFunc("POST /settings/users/{id}/enable", s.requireAdmin(s.postSetUserDisabled(false)))
	m.HandleFunc("GET /settings/audit", s.requireAuth(s.getAudit))
	m.HandleFunc("GET /settings/api-keys", s.requireAdmin(s.getAPIKeys))
	m.HandleFunc("POST /settings/api-keys", s.requireAdmin(s.postCreateAPIKey))
	m.HandleFunc("POST /settings/api-keys/{id}/revoke", s.requireAdmin(s.postRevokeAPIKey))
	m.HandleFunc("GET /settings/license", s.requireAuth(s.getLicense))
	m.HandleFunc("POST /settings/license", s.requireAdmin(s.postInstallLicense))
	m.HandleFunc("GET /settings/system", s.requireAdmin(s.getDiagnostics))
	m.HandleFunc("GET /collections", s.requireAuth(s.getCollections))
	m.HandleFunc("GET /rules", s.requireAuth(s.getRules))
	m.HandleFunc("GET /search", s.requireAuth(s.getSearch))

	s.mux = m
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}
