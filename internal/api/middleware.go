package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"github.com/nagiflow/nagipath/internal/store"
)

// cookieName and csrfToken must derive byte-for-byte the same value as
// internal/web/web.go's copies: both packages check the same cookie against
// the same CSRF token during the phased migration (docs/adr/0017), since a
// session started against a still-server-rendered page must remain valid
// against an already-ported API route, and vice versa. Kept in sync by hand
// until internal/web's own auth is deleted in Phase 7.
const cookieName = "nagipath_session"

func csrfToken(session string) string {
	sum := sha256.Sum256([]byte("nagipath-csrf:" + session))
	return hex.EncodeToString(sum[:16])
}

func checkCSRF(r *http.Request, session string) bool {
	got := r.Header.Get("X-CSRF-Token")
	return subtle.ConstantTimeCompare([]byte(got), []byte(csrfToken(session))) == 1
}

type ctxKey int

const userKey ctxKey = iota + 1

func userOf(r *http.Request) store.User {
	u, _ := r.Context().Value(userKey).(store.User)
	return u
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func apiError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

// requireAuth is the session-cookie equivalent of internal/web's auth(): same
// cookie, same CSRF check, but a JSON 401/403 instead of a redirect — a fetch
// call has nowhere to be redirected to. Does not special-case a zero-user
// database or a must-change-password account; those first-run flows are still
// owned by internal/web's /setup and /password pages until Phase 1 ports them.
func (s *Server) requireAuth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(cookieName)
		if err != nil {
			apiError(w, http.StatusUnauthorized, "unauthenticated", "Sign in required.")
			return
		}
		u, err := s.DB.SessionUser(r.Context(), c.Value)
		if err != nil {
			apiError(w, http.StatusUnauthorized, "unauthenticated", "Session expired or invalid.")
			return
		}
		if r.Method != http.MethodGet && !checkCSRF(r, c.Value) {
			apiError(w, http.StatusForbidden, "invalid_csrf", "Missing or invalid CSRF token.")
			return
		}
		h(w, r.WithContext(context.WithValue(r.Context(), userKey, u)))
	}
}

// requireAdmin wraps requireAuth and additionally requires the admin role —
// the same split internal/web's admin() enforces: viewers read, admins write.
func (s *Server) requireAdmin(h http.HandlerFunc) http.HandlerFunc {
	return s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		if !userOf(r).IsAdmin() {
			apiError(w, http.StatusForbidden, "forbidden", "This action requires an admin account.")
			return
		}
		h(w, r)
	})
}
