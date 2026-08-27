package web

import (
	"net/http"
	"strings"
)

// ---------------------------------------------------------------- first run

// getSetup is reachable only while the database has no users. Once one exists it
// is closed permanently, so it cannot become a way to add an admin later.
func (s *Server) getSetup(w http.ResponseWriter, r *http.Request) {
	if n, _ := s.DB.UserCount(r.Context()); n > 0 {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	s.render(w, r, "setup.html", "Set up nagipath", nil)
}

func (s *Server) postSetup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if n, _ := s.DB.UserCount(ctx); n > 0 {
		http.Error(w, "already set up", http.StatusForbidden)
		return
	}
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	if username == "" || len(password) < 12 {
		redirect(w, r, "/setup", "", "a username and a password of at least 12 characters are required")
		return
	}
	if password != r.FormValue("confirm") {
		redirect(w, r, "/setup", "", "the two passwords do not match")
		return
	}
	id, err := s.DB.CreateUser(ctx, username, password, "admin", username, false)
	if err != nil {
		redirect(w, r, "/setup", "", err.Error())
		return
	}
	s.DB.Audit(ctx, &id, "user.create", "user", &id, username)
	token, err := s.DB.NewSession(ctx, id, r.UserAgent(), remoteAddr(r))
	if err != nil {
		redirect(w, r, "/login", "", err.Error())
		return
	}
	s.setCookie(w, token)
	redirect(w, r, "/nodes", "welcome — add your first node", "")
}

// ---------------------------------------------------------------- login

func (s *Server) getLogin(w http.ResponseWriter, r *http.Request) {
	if n, _ := s.DB.UserCount(r.Context()); n == 0 {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return
	}
	s.render(w, r, "login.html", "Sign in", nil)
}

func (s *Server) postLogin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	username := strings.TrimSpace(r.FormValue("username"))
	// Checked before Authenticate does any argon2id work: the point of a rate
	// limit is refusing a guess before paying for it, not after — the same
	// ordering probe's rate limit uses before a Probe's request goes out.
	if limited, err := s.DB.LoginRateLimited(ctx, username); err != nil {
		redirect(w, r, "/login", "", err.Error())
		return
	} else if limited {
		s.DB.AuditDetail(ctx, nil, "auth.login", "user", nil, username, nil, "denied", remoteAddr(r))
		redirect(w, r, "/login", "", "too many failed attempts for this account; try again later")
		return
	}
	u, err := s.DB.Authenticate(ctx, username, r.FormValue("password"))
	if err != nil {
		// The message is deliberately identical for a bad password and a missing
		// user: which one it was is not the operator's business to learn here.
		s.DB.AuditDetail(ctx, nil, "auth.login", "user", nil, username, nil, "failure", remoteAddr(r))
		redirect(w, r, "/login", "", "invalid username or password")
		return
	}
	token, err := s.DB.NewSession(ctx, u.ID, r.UserAgent(), remoteAddr(r))
	if err != nil {
		redirect(w, r, "/login", "", err.Error())
		return
	}
	s.DB.AuditDetail(ctx, &u.ID, "auth.login", "user", &u.ID, u.Username, nil, "success", remoteAddr(r))
	s.setCookie(w, token)
	next := r.FormValue("next")
	if next == "" || !strings.HasPrefix(next, "/") {
		next = "/"
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (s *Server) postLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		// The same CSRF check every other mutating route gets via auth(). Checked
		// inline rather than wrapping in auth() itself, because auth() also
		// redirects a must-change-password user to /password before they ever
		// reach here — exactly the user who most needs a working sign-out button.
		if !s.checkCSRF(r, c.Value) {
			http.Error(w, "invalid or missing CSRF token", http.StatusForbidden)
			return
		}
		s.DB.EndSession(r.Context(), c.Value)
	}
	s.clearCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) getPassword(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "password.html", "Change password", nil)
}

func (s *Server) postPassword(w http.ResponseWriter, r *http.Request) {
	ctx, u := r.Context(), userOf(r)
	next := r.FormValue("new")
	if len(next) < 12 {
		redirect(w, r, "/password", "", "the new password must be at least 12 characters")
		return
	}
	if next != r.FormValue("confirm") {
		redirect(w, r, "/password", "", "the two passwords do not match")
		return
	}
	// Requiring the current password stops a borrowed logged-in browser from
	// locking the real owner out.
	if !u.MustChangePassword {
		if _, err := s.DB.Authenticate(ctx, u.Username, r.FormValue("current")); err != nil {
			redirect(w, r, "/password", "", "the current password is not correct")
			return
		}
	}
	if err := s.DB.SetPassword(ctx, u.ID, next); err != nil {
		redirect(w, r, "/password", "", err.Error())
		return
	}
	s.DB.Audit(ctx, &u.ID, "user.password_change", "user", &u.ID, u.Username)
	if err := s.DB.EndAllSessions(ctx, u.ID); err != nil {
		redirect(w, r, "/login", "", err.Error())
		return
	}
	token, err := s.DB.NewSession(ctx, u.ID, r.UserAgent(), remoteAddr(r))
	if err != nil {
		redirect(w, r, "/login", "", err.Error())
		return
	}
	s.setCookie(w, token)
	redirect(w, r, "/", "password changed; other sessions were signed out", "")
}
