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
	"strconv"
	"sync"
	"time"

	"github.com/nagiflow/nagipath/internal/collect"
	"github.com/nagiflow/nagipath/internal/keys"
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

	tpl       *template.Template
	collector *collect.Collector
	mux       *http.ServeMux

	// Live Probes, keyed by a counter. See probelive.go for why they are in memory.
	probeMu   sync.Mutex
	probeRuns map[int64]*probeRun
	probeSeq  int64
}

func New(db *store.DB, master *keys.Master, log *slog.Logger, secure, demoMode bool) (*Server, error) {
	s := &Server{DB: db, Master: master, Log: log, Secure: secure, DemoMode: demoMode}
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

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

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

const userKey ctxKey = 1

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
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, name, title string, data any) {
	p := page{Title: title, Path: r.URL.Path, User: userOf(r), Data: data}
	if c, err := r.Cookie(cookieName); err == nil {
		p.CSRF = csrfToken(c.Value)
	}
	p.Pending = s.DB.PendingHostKeyCount(r.Context())
	p.Flash = r.URL.Query().Get("ok")
	p.Error = r.URL.Query().Get("err")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
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

func idOf(r *http.Request, name string) int64 {
	n, _ := strconv.ParseInt(r.PathValue(name), 10, 64)
	return n
}

// Listen starts the server. It refuses to run without a Master Key, which is
// checked before this point: without it every stored credential is unreadable and
// a silent start would look healthy while collecting nothing.
func (s *Server) Listen(ctx context.Context, addr string) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           s,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()
	s.Log.Info("listening", "addr", addr)
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
