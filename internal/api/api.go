// Package api is the JSON surface the React SPA talks to (docs/adr/0017).
// Unlike internal/web, which renders HTML and carries the operator UI's
// business logic in its template FuncMap, this package returns plain JSON
// and every field on a response is meant to be display-ready — computed
// here, not recomputed in TypeScript. It is mounted into internal/web's
// mux at /api/, so panic recovery and security headers are inherited
// from internal/web.Server.ServeHTTP; this package adds neither.
package api

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
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
	// Secure marks the session cookie Secure (login/setup/logout/password in
	// auth.go). Mirrors internal/web.Server.Secure exactly — off for the local
	// lab's plain HTTP, on everywhere else — set directly by internal/web
	// after New, same pattern as Collector/Log above.
	Secure bool

	// Live Probes, keyed by a counter. See probelive.go for why they are in
	// memory rather than the database until they finish.
	probeMu   sync.Mutex
	probeRuns map[int64]*probeRun
	probeSeq  int64

	mux *http.ServeMux
}

func New(db *store.DB, licenseStatus func(context.Context) (license.Status, string), demoMode bool) *Server {
	s := &Server{DB: db, LicenseStatus: licenseStatus, DemoMode: demoMode}
	m := http.NewServeMux()
	// session is public: an anonymous caller gets a 200 with authenticated:false
	// (plus setup_required) rather than a 401, so the SPA's Login/Setup pages
	// can render before any session exists — see session.go's doc comment for
	// why it stays a plain handler rather than joining SessionService below.
	m.HandleFunc("GET /session", s.getSession)

	// SessionService (sessionservice.go, proto/nagipath/api/v1/session.proto).
	// Setup and Login are deliberately NOT requireAuth-wrapped (there is no
	// session yet to check) — withRequestMeta stands in for the remoteAddr/
	// User-Agent context values requireAuth would otherwise carry. Logout and
	// ChangePassword keep requireAuth's normal cookie+CSRF check.
	sessionGW := newGateway()
	if err := pb.RegisterSessionServiceHandlerServer(context.Background(), sessionGW, &sessionService{s: s}); err != nil {
		panic(err)
	}
	m.HandleFunc("POST /login", withRequestMeta(sessionGW.ServeHTTP))
	m.HandleFunc("POST /setup", withRequestMeta(sessionGW.ServeHTTP))
	m.HandleFunc("POST /logout", s.requireAuth(sessionGW.ServeHTTP))
	m.HandleFunc("POST /password", s.requireAuth(sessionGW.ServeHTTP))
	// DashboardService (dashboardservice.go, proto/nagipath/api/v1/dashboard.proto).
	dashGW := newGateway()
	if err := pb.RegisterDashboardServiceHandlerServer(context.Background(), dashGW, &dashboardService{s: s}); err != nil {
		panic(err)
	}
	m.HandleFunc("GET /dashboard", s.requireAuth(dashGW.ServeHTTP))

	// ClusterService (clusterservice.go, proto/nagipath/api/v1/clusters.proto):
	// GET /clusters and POST /clusters/rename are routed by its google.api.http
	// options, not registered here individually. requireAuth still wraps the
	// whole gateway — RenameCluster's own admin check is inside
	// clusterservice.go, since one gateway can't requireAdmin one of its two
	// routes and requireAuth the other.
	clusterGW := newGateway()
	if err := pb.RegisterClusterServiceHandlerServer(context.Background(), clusterGW, &clusterService{s: s}); err != nil {
		panic(err) // only fails on a duplicate pattern — a programming error, not a runtime condition
	}
	m.HandleFunc("GET /clusters", s.requireAuth(clusterGW.ServeHTTP))
	m.HandleFunc("POST /clusters/rename", s.requireAuth(clusterGW.ServeHTTP))

	// SiteService (siteservice.go, proto/nagipath/api/v1/sites.proto).
	// ?export=csv stays outside the gateway (gateway.go's gatewayOrCSV).
	siteGW := newGateway()
	if err := pb.RegisterSiteServiceHandlerServer(context.Background(), siteGW, &siteService{s: s}); err != nil {
		panic(err)
	}
	m.HandleFunc("GET /sites", s.requireAuth(gatewayOrCSV(siteGW, s.getSitesCSV)))
	m.HandleFunc("GET /sites/{name}", s.requireAuth(siteGW.ServeHTTP))

	// NodeService (nodeservice.go, proto/nagipath/api/v1/nodes.proto).
	// requireAuth wraps the whole gateway; every RPC but ListNodes and
	// GetNode calls requireAdminRPC as its own first line — same reasoning
	// as DriftService's mutations below.
	nodeGW := newGateway()
	if err := pb.RegisterNodeServiceHandlerServer(context.Background(), nodeGW, &nodeService{s: s}); err != nil {
		panic(err)
	}
	m.HandleFunc("GET /nodes", s.requireAuth(nodeGW.ServeHTTP))
	m.HandleFunc("POST /nodes", s.requireAuth(nodeGW.ServeHTTP))
	m.HandleFunc("POST /nodes/import", s.requireAuth(nodeGW.ServeHTTP))
	m.HandleFunc("GET /nodes/{id}", s.requireAuth(nodeGW.ServeHTTP))
	// The live-file and restart routes are the write surface (sshx.Client's
	// WriteFile and Restart); nodeservice.go's own requireAdminRPC gates them,
	// like every other mutation on this gateway.
	m.HandleFunc("GET /nodes/{id}/livefile", s.requireAuth(nodeGW.ServeHTTP))
	m.HandleFunc("POST /nodes/{id}/livefile", s.requireAuth(nodeGW.ServeHTTP))
	m.HandleFunc("POST /instances/{id}/restart", s.requireAuth(nodeGW.ServeHTTP))
	m.HandleFunc("GET /nodes/{id}/{tab}", s.requireAuth(nodeGW.ServeHTTP))
	m.HandleFunc("POST /nodes/{id}/collect", s.requireAuth(nodeGW.ServeHTTP))
	m.HandleFunc("POST /nodes/{id}/delete", s.requireAuth(nodeGW.ServeHTTP))
	m.HandleFunc("POST /nodes/{id}/credential", s.requireAuth(nodeGW.ServeHTTP))
	m.HandleFunc("POST /hostkeys/{id}/decide", s.requireAuth(nodeGW.ServeHTTP))
	// DriftService (driftservice.go, proto/nagipath/api/v1/drift.proto).
	// requireAuth wraps the whole gateway; the four mutations' admin checks
	// are each RPC's own first line (requireAdminRPC, middleware.go) — same
	// reasoning as ClusterService.RenameCluster above.
	driftGW := newGateway()
	if err := pb.RegisterDriftServiceHandlerServer(context.Background(), driftGW, &driftService{s: s}); err != nil {
		panic(err)
	}
	m.HandleFunc("GET /drift", s.requireAuth(driftGW.ServeHTTP))
	m.HandleFunc("GET /drift/review/{instanceID}", s.requireAuth(driftGW.ServeHTTP))
	m.HandleFunc("POST /drift/recompute", s.requireAuth(driftGW.ServeHTTP))
	m.HandleFunc("POST /drift/ignore", s.requireAuth(driftGW.ServeHTTP))
	m.HandleFunc("POST /drift/ignore/{id}/delete", s.requireAuth(driftGW.ServeHTTP))
	m.HandleFunc("POST /drift/golden", s.requireAuth(driftGW.ServeHTTP))
	// CertificateService (certificateservice.go, proto/nagipath/api/v1/certificates.proto).
	certGW := newGateway()
	if err := pb.RegisterCertificateServiceHandlerServer(context.Background(), certGW, &certificateService{s: s}); err != nil {
		panic(err)
	}
	m.HandleFunc("GET /certificates", s.requireAuth(gatewayOrCSV(certGW, s.getCertificatesCSV)))
	m.HandleFunc("GET /certificates/{id}", s.requireAuth(certGW.ServeHTTP))

	// SnapshotService (snapshotservice.go, proto/nagipath/api/v1/snapshots.proto).
	snapGW := newGateway()
	if err := pb.RegisterSnapshotServiceHandlerServer(context.Background(), snapGW, &snapshotService{s: s}); err != nil {
		panic(err)
	}
	m.HandleFunc("GET /snapshots", s.requireAuth(gatewayOrCSV(snapGW, s.getSnapshotsCSV)))
	m.HandleFunc("GET /snapshots/{id}/file/{fileID}", s.requireAuth(snapGW.ServeHTTP))

	// SettingsService (settingsservice.go, proto/nagipath/api/v1/settings.proto).
	// requireAuth wraps the whole gateway; every RPC but GetAudit and
	// GetLicense calls requireAdminRPC as its own first line — same
	// reasoning as DriftService's mutations above. GET /settings/audit
	// ?export=csv stays outside the gateway (gateway.go's gatewayOrCSV).
	settingsGW := newGateway()
	if err := pb.RegisterSettingsServiceHandlerServer(context.Background(), settingsGW, &settingsService{s: s}); err != nil {
		panic(err)
	}
	m.HandleFunc("GET /settings/credentials", s.requireAuth(settingsGW.ServeHTTP))
	m.HandleFunc("POST /settings/credentials", s.requireAuth(settingsGW.ServeHTTP))
	m.HandleFunc("GET /settings/hostkeys", s.requireAuth(settingsGW.ServeHTTP))
	m.HandleFunc("GET /settings/masterkey", s.requireAuth(settingsGW.ServeHTTP))
	m.HandleFunc("GET /settings/collection-defaults", s.requireAuth(settingsGW.ServeHTTP))
	m.HandleFunc("POST /settings/collection-defaults", s.requireAuth(settingsGW.ServeHTTP))
	m.HandleFunc("GET /settings/retention", s.requireAuth(settingsGW.ServeHTTP))
	m.HandleFunc("POST /settings/retention", s.requireAuth(settingsGW.ServeHTTP))
	m.HandleFunc("POST /settings/retention/prune", s.requireAuth(settingsGW.ServeHTTP))
	m.HandleFunc("GET /settings/users", s.requireAuth(settingsGW.ServeHTTP))
	m.HandleFunc("POST /settings/users", s.requireAuth(settingsGW.ServeHTTP))
	m.HandleFunc("POST /settings/users/{id}/disable", s.requireAuth(settingsGW.ServeHTTP))
	m.HandleFunc("POST /settings/users/{id}/enable", s.requireAuth(settingsGW.ServeHTTP))
	m.HandleFunc("GET /settings/audit", s.requireAuth(gatewayOrCSV(settingsGW, s.getAuditCSV)))
	m.HandleFunc("GET /settings/api-keys", s.requireAuth(settingsGW.ServeHTTP))
	m.HandleFunc("POST /settings/api-keys", s.requireAuth(settingsGW.ServeHTTP))
	m.HandleFunc("POST /settings/api-keys/{id}/revoke", s.requireAuth(settingsGW.ServeHTTP))
	m.HandleFunc("GET /settings/license", s.requireAuth(settingsGW.ServeHTTP))
	m.HandleFunc("POST /settings/license", s.requireAuth(settingsGW.ServeHTTP))
	m.HandleFunc("GET /settings/system", s.requireAuth(settingsGW.ServeHTTP))
	// CollectionService (collectionservice.go, proto/nagipath/api/v1/settings.proto).
	collGW := newGateway()
	if err := pb.RegisterCollectionServiceHandlerServer(context.Background(), collGW, &collectionService{s: s}); err != nil {
		panic(err)
	}
	m.HandleFunc("GET /collections", s.requireAuth(gatewayOrCSV(collGW, s.getCollectionsCSV)))

	// RuleService (ruleservice.go, proto/nagipath/api/v1/rules.proto).
	ruleGW := newGateway()
	if err := pb.RegisterRuleServiceHandlerServer(context.Background(), ruleGW, &ruleService{s: s}); err != nil {
		panic(err)
	}
	m.HandleFunc("GET /rules", s.requireAuth(gatewayOrCSV(ruleGW, s.getRulesCSV)))

	// SearchService (searchservice.go, proto/nagipath/api/v1/search.proto).
	searchGW := newGateway()
	if err := pb.RegisterSearchServiceHandlerServer(context.Background(), searchGW, &searchService{s: s}); err != nil {
		panic(err)
	}
	m.HandleFunc("GET /search", s.requireAuth(searchGW.ServeHTTP))

	// TraceService (traceservice.go, proto/nagipath/api/v1/trace.proto).
	// requireAuth wraps the whole gateway; StartProbe calls requireAdminRPC
	// as its own first line — same reasoning as DriftService's mutations.
	traceGW := newGateway()
	if err := pb.RegisterTraceServiceHandlerServer(context.Background(), traceGW, &traceService{s: s}); err != nil {
		panic(err)
	}
	m.HandleFunc("GET /trace", s.requireAuth(traceGW.ServeHTTP))
	m.HandleFunc("POST /trace", s.requireAuth(traceGW.ServeHTTP))
	m.HandleFunc("POST /trace/probe", s.requireAuth(traceGW.ServeHTTP))
	m.HandleFunc("GET /trace/run/{id}", s.requireAuth(traceGW.ServeHTTP))

	// ProbeService (probeservice.go, proto/nagipath/api/v1/probe.proto).
	// GetProbeDetail calls requireAdminRPC as its own first line. GET
	// /trace/history?export=csv stays outside the gateway (gateway.go's
	// gatewayOrCSV).
	probeGW := newGateway()
	if err := pb.RegisterProbeServiceHandlerServer(context.Background(), probeGW, &probeService{s: s}); err != nil {
		panic(err)
	}
	m.HandleFunc("GET /trace/probe/{id}", s.requireAuth(probeGW.ServeHTTP))
	m.HandleFunc("GET /trace/history", s.requireAuth(gatewayOrCSV(probeGW, s.getProbeHistoryCSV)))

	s.mux = m
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}
