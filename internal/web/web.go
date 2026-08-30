// Package web is the HTTP server: it serves the embedded React SPA (spa.go)
// and mounts internal/api's JSON surface at /api/ — one entry point for both
// the SPA's session cookie and external Bearer-token callers, same handlers
// and same response shapes either way (internal/api's requireAuth accepts
// both) — no HTML rendering of its own.
// Every page is the SPA now (docs/adr/0017, Phase 9); the whole point of a
// single binary is still that `nagipath server` is the only thing an
// operator installs, so the UI ships inside it.
package web

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nagiflow/nagipath/internal/api"
	"github.com/nagiflow/nagipath/internal/collect"
	"github.com/nagiflow/nagipath/internal/keys"
	"github.com/nagiflow/nagipath/internal/license"
	"github.com/nagiflow/nagipath/internal/sshx"
	"github.com/nagiflow/nagipath/internal/store"
)

const cookieName = "nagipath_session"

type Server struct {
	DB     *store.DB
	Master *keys.Master
	Log    *slog.Logger
	// Secure marks the session cookie Secure. It is off for the local lab, which
	// runs over plain HTTP, and on everywhere else.
	Secure bool
	// DemoMode refuses every Probe outright (probe.md §2, PRD-V1.md §8): a
	// public-facing trial instance must never send a real outbound request.
	DemoMode bool
	// License is nil when no license file was found or it failed to load —
	// that is a valid, soft-enforced state (ADR-0014), never a startup
	// failure. See licenseStatus for how a nil License is treated. Read on
	// every request and written when an admin installs a new one from the
	// UI, so every access goes through licenseMu — see currentLicense/setLicense.
	License   *license.License
	licenseMu sync.RWMutex
	// LicensePath is where the running server was told to load its license
	// from (the -license/NAGIPATH_LICENSE_FILE value); installLicense writes
	// a newly pasted license here so it survives a restart.
	LicensePath string
	// MetricsToken gates GET /metrics. Empty means the endpoint is off (404):
	// a security-conscious default, since fleet-internal counts should only be
	// exposed once an operator deliberately turns them on.
	MetricsToken string

	// StartedAt, ListenAddr and TLSEnabled feed the /diagnostics panel
	// (docs/frontend/settings.md §10). New's parameter list is already at 8;
	// cmdServer sets these three directly on the returned *Server instead of
	// growing it further.
	StartedAt  time.Time
	ListenAddr string
	TLSEnabled bool
	// Logs captures the process's recent log lines for the diagnostics bundle.
	// nil unless cmdServer wired one in — only `nagipath server` needs it.
	Logs *RingBuffer

	collector *collect.Collector
	mux       *http.ServeMux
	// api is the JSON surface the SPA calls, mounted at /api/ (docs/adr/0017).
	// It shares s.DB and reads license state through s.LicenseStatus rather than
	// holding its own copy — see internal/api.Server's doc comment.
	api *api.Server

	// Process-lifetime request counters for /metrics. atomic because every
	// request touches them, with no other synchronisation.
	httpRequests        atomic.Int64
	httpRequestDurMSSum atomic.Int64
}

func New(db *store.DB, master *keys.Master, log *slog.Logger, secure, demoMode bool, lic *license.License, metricsToken string, licensePath string) (*Server, error) {
	s := &Server{DB: db, Master: master, Log: log, Secure: secure, DemoMode: demoMode, License: lic,
		MetricsToken: metricsToken, LicensePath: licensePath}
	s.collector = &collect.Collector{DB: db, Dialer: collect.SSH{
		Dialer: &sshx.Dialer{DB: db, Master: master, Timeout: 20 * time.Second},
	}}
	api.Version = Version
	s.api = api.New(db, s.LicenseStatus, demoMode)
	s.api.Collector = s.collector
	s.api.Log = log
	s.api.Master = master
	s.api.LicensePath = licensePath
	s.api.Secure = secure
	s.api.CurrentLicense = s.currentLicense
	s.api.InstallLicense = s.installLicenseBytes
	// StartedAt/ListenAddr/TLSEnabled are set on s by cmdServer after New
	// returns (see the Server.StartedAt doc comment), so the diagnostics page
	// reads them through a closure rather than a value copied too early.
	s.api.Diag = func() (time.Time, string, bool) { return s.StartedAt, s.ListenAddr, s.TLSEnabled }
	s.routes()
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	rw := &recoveringWriter{ResponseWriter: w}
	// A panic anywhere below — a handler, a template, a store call — must not take
	// the process down with it: this is the one request boundary every request
	// crosses, so it is the one place recovery belongs (nothing per-handler).
	defer func() {
		if rec := recover(); rec != nil {
			s.Log.Error("panic recovered", "method", r.Method, "path", r.URL.Path,
				"panic", rec, "stack", string(debug.Stack()))
			// A panic after the response was already partially written cannot be
			// un-sent; writing again here would just log a second, noisier error.
			if !rw.wrote {
				http.Error(rw, "Internal Server Error", http.StatusInternalServerError)
			}
		}
		// Process-lifetime totals for /metrics: every request, once, after it
		// completes — no per-route or per-status breakdown (out of scope).
		s.httpRequests.Add(1)
		s.httpRequestDurMSSum.Add(time.Since(start).Milliseconds())
	}()
	s.securityHeaders(w)
	status, message := s.licenseStatus(r.Context())
	// ADR-0014: soft enforcement means a header and a banner, on every
	// response, and nothing that could ever refuse to serve one.
	w.Header().Set("X-Nagipath-License", string(status))
	r = r.WithContext(context.WithValue(r.Context(), licenseKey, message))
	s.mux.ServeHTTP(rw, r)
}

// securityHeaders is set on every response, not just the SPA shell — /metrics,
// /healthz and the SPA's static assets get them too, since a security scanner
// checks the response, not the route.
//
// script-src is 'self', not 'none': the only script the UI ever loads is its
// own embedded, same-origin React bundle (registerSPAAssets in spa.go) — no
// inline script, no eval, no external origin, so 'self' with no
// 'unsafe-inline' is the whole budget. style-src keeps 'unsafe-inline' since
// Emotion (EUI's styling primitive) injects inline `style` attributes.
func (s *Server) securityHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "same-origin")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Content-Security-Policy",
		"default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'; "+
			"frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
	// Only asserted when this process was told it's behind TLS (-secure-cookies,
	// the same flag session cookies key Secure off of) — sending it over plain
	// HTTP would be a lie the browser has no way to check.
	if s.Secure {
		h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
	}
}

// recoveringWriter tracks whether a response has actually started, so the panic
// recovery in ServeHTTP knows whether it is still safe to write a 500.
type recoveringWriter struct {
	http.ResponseWriter
	wrote bool
}

func (w *recoveringWriter) WriteHeader(code int) {
	w.wrote = true
	w.ResponseWriter.WriteHeader(code)
}

func (w *recoveringWriter) Write(b []byte) (int, error) {
	w.wrote = true
	return w.ResponseWriter.Write(b)
}

// LicenseStatus is licenseStatus exported for cmdServer's one startup audit
// entry (ADR-0014); everything per-request uses the unexported form above.
func (s *Server) LicenseStatus(ctx context.Context) (license.Status, string) {
	return s.licenseStatus(ctx)
}

// licenseStatus computes the current license Status and its human-readable
// message. It costs one cheap COUNT(*) query per request; a nil License
// (missing or failed to load at startup) is its own status, not an error.
func (s *Server) licenseStatus(ctx context.Context) (license.Status, string) {
	lic := s.currentLicense()
	if lic == nil {
		return license.Missing, license.Missing.Message()
	}
	n, err := s.DB.NodeCount(ctx)
	if err != nil {
		n = 0
	}
	st := lic.Status(n, time.Now())
	return st, st.Message()
}

// currentLicense and setLicense guard Server.License: every request reads it
// (licenseStatus, above) and an admin installing a new one from /license
// writes it, concurrently with those reads.
func (s *Server) currentLicense() *license.License {
	s.licenseMu.RLock()
	defer s.licenseMu.RUnlock()
	return s.License
}

func (s *Server) setLicense(lic *license.License) {
	s.licenseMu.Lock()
	defer s.licenseMu.Unlock()
	s.License = lic
}

func (s *Server) routes() {
	m := http.NewServeMux()
	s.registerSPAAssets(m)
	s.routesCore(m)
	s.routesSPA(m)
	s.mux = m
}

// routesCore is everything outside the navigation: assets, the unauthenticated
// entry points, the two probes a load balancer reads, and the operator's own
// account page.
func (s *Server) routesCore(m *http.ServeMux) {
	// Login and setup are the SPA now (docs/adr/0017, Phase 8): the actual
	// UserCount()==0/already-authenticated branching that used to happen here
	// moved client-side into GET /api/session's authenticated/setup_required
	// fields (internal/api/session.go) plus each page's own <Navigate> guard.
	// Deliberately NOT s.auth-wrapped: auth()'s missing-cookie branch redirects
	// to /login itself, which for these two paths would loop forever.
	m.HandleFunc("GET /login", s.serveSPA)
	m.HandleFunc("GET /setup", s.serveSPA)
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	m.HandleFunc("GET /readyz", s.readyz)
	m.HandleFunc("GET /metrics", s.metrics)
	// internal/api's JSON surface, one entry point for both callers — the SPA's
	// session cookie and external Bearer-token callers alike. requireAuth
	// (internal/api/middleware.go) is what tells them apart, not the path.
	m.Handle("/api/", http.StripPrefix("/api", s.api))

	// Password is the SPA now too: business logic lives at POST /api/password
	// (internal/api/auth.go). Plain s.auth(s.serveSPA), same as every other
	// authenticated page — auth()'s existing must-change-password bypass for
	// path=="/password" (below) keeps working unchanged since it matches on
	// the path, not the handler.
	m.HandleFunc("GET /password", s.auth(s.serveSPA))

	// Catch-all, last because every other pattern is more specific than "/".
	// Behind auth so a mistyped URL sends a stranger to the login page rather
	// than telling them which paths this deployment does not serve.
	m.HandleFunc("/", s.auth(s.notFound))
}

// ---------------------------------------------------------------- middleware

type ctxKey int

const (
	userKey ctxKey = iota + 1
	licenseKey
)

func userOf(r *http.Request) store.User {
	u, _ := r.Context().Value(userKey).(store.User)
	return u
}

func (s *Server) auth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if n, _ := s.DB.UserCount(r.Context()); n == 0 {
			http.Redirect(w, r, "/setup", http.StatusSeeOther)
			return
		}
		c, err := r.Cookie(cookieName)
		if err != nil {
			http.Redirect(w, r, "/login?next="+r.URL.Path, http.StatusSeeOther)
			return
		}
		u, err := s.DB.SessionUser(r.Context(), c.Value)
		if err != nil {
			s.clearCookie(w)
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		// A user who must change their password can reach exactly one page.
		if u.MustChangePassword && r.URL.Path != "/password" {
			http.Redirect(w, r, "/password", http.StatusSeeOther)
			return
		}
		if r.Method != http.MethodGet && !s.checkCSRF(r, c.Value) {
			http.Error(w, "invalid or missing CSRF token", http.StatusForbidden)
			return
		}
		h(w, r.WithContext(context.WithValue(r.Context(), userKey, u)))
	}
}

// admin wraps auth and additionally requires the admin role. Viewers can read
// everything and change nothing: that split is the whole authorisation model.
func (s *Server) admin(h http.HandlerFunc) http.HandlerFunc {
	return s.auth(func(w http.ResponseWriter, r *http.Request) {
		if !userOf(r).IsAdmin() {
			http.Error(w, "this action requires an admin account", http.StatusForbidden)
			return
		}
		h(w, r)
	})
}

// csrfToken is derived from the session token, so it needs no server-side state
// and dies with the session. A stolen CSRF token is useless without the cookie.
func csrfToken(session string) string {
	sum := sha256.Sum256([]byte("nagipath-csrf:" + session))
	return hex.EncodeToString(sum[:16])
}

func (s *Server) checkCSRF(r *http.Request, session string) bool {
	got := r.FormValue("csrf")
	if got == "" {
		got = r.Header.Get("X-CSRF-Token")
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(csrfToken(session))) == 1
}

// clearCookie is auth()'s only remaining caller: login/setup/logout/password
// now issue and clear this cookie from internal/api/auth.go instead (its own
// setCookie/clearCookie, same shape) — auth() still needs to clear it here
// when a stale cookie's SessionUser lookup fails.
func (s *Server) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: "", Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: s.Secure, MaxAge: -1,
	})
}

// notFound is the 404 every route uses: a wrong id or a mistyped URL keeps the
// operator inside the product, with the sidebar they navigate by, instead of
// dropping them onto a plain-text page with no way back. The SPA's wildcard
// route renders the actual "not found" content; this just has to send a real
// 404 status ahead of it — serveSPA never calls WriteHeader itself, so the
// first call here wins.
func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotFound)
	s.serveSPA(w, r)
}

// serverError logs the real error (with route context) and sends the client a
// generic message. err.Error() can carry SQL driver detail, file paths, or other
// internals that were never meant to reach a browser — see the security-review
// finding this fixes.
func (s *Server) serverError(w http.ResponseWriter, r *http.Request, err error) {
	s.Log.Error("internal error", "method", r.Method, "path", r.URL.Path, "err", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}

// Listen starts the server. It refuses to run without a Master Key, which is
// checked before this point: without it every stored credential is unreadable and
// a silent start would look healthy while collecting nothing.
//
// certFile and keyFile are docs/infra/customer_deployment.md's "Direct TLS"
// option; both empty is "Plain HTTP" — permitted, but session cookies cannot be
// marked Secure over it, so that gets its own startup line rather than a silent
// downgrade.
func (s *Server) Listen(ctx context.Context, addr, certFile, keyFile string) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           s,
		ReadHeaderTimeout: 10 * time.Second,
		// A slow client trickling bytes must not hold a worker goroutine forever.
		// WriteTimeout is generous because it covers the largest legitimate
		// response — a snapshot config file served from a blob — not just a
		// typical page.
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()
	if certFile != "" || keyFile != "" {
		if certFile == "" || keyFile == "" {
			return errors.New("both NAGIPATH_TLS_CERT and NAGIPATH_TLS_KEY are required to enable direct TLS")
		}
		s.Log.Info("listening", "addr", addr, "tls", true)
		if err := srv.ListenAndServeTLS(certFile, keyFile); !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
	if !s.Secure {
		s.Log.Info("listening on plain HTTP: session cookies cannot be marked Secure " +
			"(behind a TLS-terminating reverse proxy, set -secure-cookies)")
	}
	s.Log.Info("listening", "addr", addr, "tls", false)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func remoteAddr(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
