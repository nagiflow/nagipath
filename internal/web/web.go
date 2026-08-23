// Package web serves the operator UI: server-rendered HTML, no build step, no
// JavaScript bundle. The whole point of a single binary is that `nagipath server`
// is the only thing an operator installs, so the UI ships inside it.
package web

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"errors"
	"html/template"
	"log/slog"
	"net"
	"net/http"
	neturl "net/url"
	"runtime/debug"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nagiflow/nagipath/internal/collect"
	"github.com/nagiflow/nagipath/internal/keys"
	"github.com/nagiflow/nagipath/internal/license"
	"github.com/nagiflow/nagipath/internal/sshx"
	"github.com/nagiflow/nagipath/internal/store"
)

//go:embed templates/*.html templates/parts/*.html static/*
var assets embed.FS

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

	tpl       *template.Template
	collector *collect.Collector
	mux       *http.ServeMux

	// Live Probes, keyed by a counter. See probelive.go for why they are in memory.
	probeMu   sync.Mutex
	probeRuns map[int64]*probeRun
	probeSeq  int64

	// Process-lifetime request counters for /metrics. atomic because every
	// request touches them, with no other synchronisation.
	httpRequests        atomic.Int64
	httpRequestDurMSSum atomic.Int64
}

func New(db *store.DB, master *keys.Master, log *slog.Logger, secure, demoMode bool, lic *license.License, metricsToken string, licensePath string) (*Server, error) {
	s := &Server{DB: db, Master: master, Log: log, Secure: secure, DemoMode: demoMode, License: lic,
		MetricsToken: metricsToken, LicensePath: licensePath}
	tpl, err := template.New("").Funcs(funcs).ParseFS(assets, "templates/*.html", "templates/parts/*.html")
	if err != nil {
		return nil, err
	}
	s.tpl = tpl
	s.collector = &collect.Collector{DB: db, Dialer: collect.SSH{
		Dialer: &sshx.Dialer{DB: db, Master: master, Timeout: 20 * time.Second},
	}}
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

// securityHeaders is set on every response, not just rendered pages — /metrics,
// /healthz and static assets get them too, since a security scanner checks the
// response, not the route. The UI ships zero JavaScript, anywhere (see the
// "no script" comments throughout internal/web/templates) — script-src 'none'
// is therefore not a compromise, it's a fact about this codebase. style-src
// needs 'unsafe-inline' because templates use style="..." attributes rather
// than a stylesheet-only discipline; that's the one real relaxation here.
func (s *Server) securityHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "same-origin")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Content-Security-Policy",
		"default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'none'; "+
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
	m.Handle("GET /static/", http.FileServerFS(assets))

	// Open routes: setup runs only while there are no users, login always.
	m.HandleFunc("GET /setup", s.getSetup)
	m.HandleFunc("POST /setup", s.postSetup)
	m.HandleFunc("GET /login", s.getLogin)
	m.HandleFunc("POST /login", s.postLogin)
	m.HandleFunc("POST /logout", s.postLogout)
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	m.HandleFunc("GET /readyz", s.readyz)
	m.HandleFunc("GET /metrics", s.metrics)

	m.HandleFunc("GET /{$}", s.auth(s.dashboard))
	m.HandleFunc("GET /password", s.auth(s.getPassword))
	m.HandleFunc("POST /password", s.auth(s.postPassword))

	m.HandleFunc("GET /nodes", s.auth(s.nodes))
	m.HandleFunc("POST /nodes", s.admin(s.addNode))
	m.HandleFunc("GET /nodes/{id}", s.auth(s.node))
	m.HandleFunc("POST /nodes/{id}/collect", s.admin(s.collectNode))
	m.HandleFunc("POST /nodes/{id}/delete", s.admin(s.deleteNode))
	m.HandleFunc("POST /hostkeys/{id}/decide", s.admin(s.decideHostKey))
	m.HandleFunc("POST /hostkeys/approve", s.admin(s.approveHostKeys))

	m.HandleFunc("GET /fleet", s.auth(s.fleet))
	m.HandleFunc("GET /onboarding", s.admin(s.onboarding))
	m.HandleFunc("POST /onboarding/nodes", s.admin(s.onboardNodes))

	m.HandleFunc("GET /credentials", s.admin(s.credentials))
	m.HandleFunc("POST /credentials", s.admin(s.addCredential))

	// Viewers can see license status too (docs/frontend/settings.md); only
	// admins can install a new one.
	m.HandleFunc("GET /license", s.auth(s.license))
	m.HandleFunc("POST /license", s.admin(s.installLicense))

	// Admin-only, unlike /license: docs/frontend/settings.md's role column
	// puts /settings/system at admin, and a diagnostics bundle is exactly the
	// kind of thing a viewer should not be handed a download link for.
	m.HandleFunc("GET /diagnostics", s.admin(s.diagnostics))
	m.HandleFunc("GET /diagnostics/bundle", s.admin(s.diagnosticsBundle))

	m.HandleFunc("GET /users", s.admin(s.users))
	m.HandleFunc("POST /users", s.admin(s.addUser))
	m.HandleFunc("POST /users/{id}/disable", s.admin(s.disableUser))
	m.HandleFunc("POST /users/{id}/enable", s.admin(s.enableUser))

	m.HandleFunc("GET /instances", s.auth(s.instances))
	// A cluster is discovered from the configuration and is the level above a node,
	// so it has no screen of its own and no membership form — only a name.
	m.HandleFunc("POST /clusters/rename", s.admin(s.renameCluster))
	m.HandleFunc("GET /instances/{id}", s.auth(s.instance))
	m.HandleFunc("GET /snapshots/{id}/file/{fileID}", s.auth(s.snapshotFile))

	m.HandleFunc("GET /collections", s.auth(s.collections))
	m.HandleFunc("GET /certificates", s.auth(s.certificates))
	m.HandleFunc("GET /audit", s.admin(s.audit))
	m.HandleFunc("GET /search", s.auth(s.search))

	m.HandleFunc("POST /trace/probe", s.admin(s.startTraceProbe))
	m.HandleFunc("GET /trace", s.auth(s.trace))
	m.HandleFunc("POST /trace", s.auth(s.trace))
	m.HandleFunc("GET /rules", s.auth(s.rules))

	m.HandleFunc("GET /drift", s.auth(s.drift))
	m.HandleFunc("POST /drift/recompute", s.admin(s.driftRecompute))
	m.HandleFunc("POST /drift/ignore", s.admin(s.driftIgnore))
	m.HandleFunc("POST /drift/ignore/{id}/delete", s.admin(s.driftUnignore))
	m.HandleFunc("POST /drift/golden", s.admin(s.driftGolden))

	s.mux = m
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

func (s *Server) setCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: token, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: s.Secure, MaxAge: int(12 * time.Hour / time.Second),
	})
}

func (s *Server) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: "", Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: s.Secure, MaxAge: -1,
	})
}

// ---------------------------------------------------------------- rendering

type page struct {
	Title    string
	Path     string
	User     store.User
	CSRF     string
	Pending  int
	Flash    string
	Error    string
	Data     any
	NextPath string
	// LicenseNotice is the human-readable warning for the banner near the
	// top of the page. Empty when the license is valid, so layout.html can
	// gate the banner on this alone.
	LicenseNotice string
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, name, title string, data any) {
	p := page{Title: title, Path: r.URL.Path, User: userOf(r), Data: data}
	if c, err := r.Cookie(cookieName); err == nil {
		p.CSRF = csrfToken(c.Value)
	}
	p.Pending = s.DB.PendingHostKeyCount(r.Context())
	p.LicenseNotice, _ = r.Context().Value(licenseKey).(string)
	p.Flash = r.URL.Query().Get("ok")
	p.Error = r.URL.Query().Get("err")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tpl.ExecuteTemplate(w, name, p); err != nil {
		s.Log.Error("render", "template", name, "err", err)
	}
}

// redirect carries a one-line result in the query string. It is the laziest flash
// message that survives a redirect without a session store.
func redirect(w http.ResponseWriter, r *http.Request, path, ok, errMsg string) {
	q := ""
	switch {
	case errMsg != "":
		q = "?err=" + neturl.QueryEscape(errMsg)
	case ok != "":
		q = "?ok=" + neturl.QueryEscape(ok)
	}
	http.Redirect(w, r, path+q, http.StatusSeeOther)
}

// serverError logs the real error (with route context) and sends the client a
// generic message. err.Error() can carry SQL driver detail, file paths, or other
// internals that were never meant to reach a browser — see the security-review
// finding this fixes.
func (s *Server) serverError(w http.ResponseWriter, r *http.Request, err error) {
	s.Log.Error("internal error", "method", r.Method, "path", r.URL.Path, "err", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}

func idOf(r *http.Request, name string) int64 {
	n, _ := strconv.ParseInt(r.PathValue(name), 10, 64)
	return n
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
