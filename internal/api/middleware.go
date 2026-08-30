package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/nagiflow/nagipath/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// cookieName and csrfToken must derive byte-for-byte the same value as
// internal/web/web.go's copies: internal/web's auth() middleware still gates
// every already-ported SPA page using this same cookie and CSRF token, even
// though login/setup/logout/password themselves moved here in Phase 8. Kept
// in sync by hand — internal/web has no reason to import this package.
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

const (
	userKey ctxKey = iota + 1
	sessionTokenKey
	remoteAddrKey
	userAgentKey
)

// userOf takes a context rather than a *http.Request so it works the same
// way from an ordinary handler (userOf(r.Context())) and from a
// ClusterService-style RPC method, which only ever has a context — see
// clusterservice.go.
func userOf(ctx context.Context) store.User {
	u, _ := ctx.Value(userKey).(store.User)
	return u
}

// sessionTokenOf is the raw session-cookie value behind userOf's resolved
// store.User — set only on the cookie branch of requireAuth (a Bearer call
// has no session to end), and only meaningfully used by
// sessionservice.go's Logout, which needs the token itself to end that one
// session rather than every session the user holds.
func sessionTokenOf(ctx context.Context) string {
	tok, _ := ctx.Value(sessionTokenKey).(string)
	return tok
}

// remoteAddrOf and userAgentOf are r.RemoteAddr (via remoteAddr, target.go)
// and r.UserAgent() for an RPC method, which only ever gets a context — a
// gRPC transport wouldn't carry either at all (they're connection-level, not
// headers grpc-gateway would forward through metadata), so withRequestMeta
// puts them on the context itself, upstream of the gateway, the same way
// requireAuth already does for userKey.
func remoteAddrOf(ctx context.Context) string {
	a, _ := ctx.Value(remoteAddrKey).(string)
	return a
}

func userAgentOf(ctx context.Context) string {
	a, _ := ctx.Value(userAgentKey).(string)
	return a
}

// withRequestMeta puts remoteAddr(r) and r.UserAgent() on the request
// context — needed by the SessionService RPCs that aren't requireAuth-
// wrapped (Setup, Login: there is no session yet to check), which otherwise
// get neither since requireAuth is what normally carries this.
func withRequestMeta(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), remoteAddrKey, remoteAddr(r))
		ctx = context.WithValue(ctx, userAgentKey, r.UserAgent())
		h(w, r.WithContext(ctx))
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func apiError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

// requireAuth accepts either of this API's two callers: the SPA, which sends
// the session cookie and needs the CSRF check since a browser attaches
// cookies automatically; and a Bearer API token (mounted at /api/ too —
// see internal/web.Server.routes), which needs no CSRF check since nothing
// attaches an Authorization header without the caller meaning to. Same
// handlers either way — the two mount points differ only in which of these
// this middleware accepts, not in the JSON either produces. A JSON 401/403 is
// returned rather than a redirect, since a fetch or an API client has nowhere
// to be redirected to. Does not special-case a zero-user database
// (getSession/postSetup in auth.go handle that) or a must-change-password
// account (internal/web's auth() still enforces that redirect for every page
// except /password itself, which stays a plain s.auth(s.serveSPA) route —
// Bearer callers have no notion of a page to redirect from in the first
// place).
func (s *Server) requireAuth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
			u, err := s.DB.AuthenticateAPIToken(r.Context(), strings.TrimSpace(raw))
			if err != nil {
				apiError(w, http.StatusUnauthorized, "invalid_token", "The API key is invalid, expired or revoked.")
				return
			}
			ctx := context.WithValue(r.Context(), userKey, u)
			ctx = context.WithValue(ctx, remoteAddrKey, remoteAddr(r))
			ctx = context.WithValue(ctx, userAgentKey, r.UserAgent())
			h(w, r.WithContext(ctx))
			return
		}

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
		ctx := context.WithValue(r.Context(), userKey, u)
		ctx = context.WithValue(ctx, sessionTokenKey, c.Value)
		ctx = context.WithValue(ctx, remoteAddrKey, remoteAddr(r))
		ctx = context.WithValue(ctx, userAgentKey, r.UserAgent())
		h(w, r.WithContext(ctx))
	}
}

// requireAdmin wraps requireAuth and additionally requires the admin role —
// the same split internal/web's admin() enforces: viewers read, admins write.
func (s *Server) requireAdmin(h http.HandlerFunc) http.HandlerFunc {
	return s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		if !userOf(r.Context()).IsAdmin() {
			apiError(w, http.StatusForbidden, "forbidden", "This action requires an admin account.")
			return
		}
		h(w, r)
	})
}

// requireAdminRPC is requireAdmin's counterpart for an RPC method mixed into
// a gateway that only requireAuth wraps at the http level (a domain with
// both viewer-ok and admin-only RPCs sharing one *runtime.ServeMux, e.g.
// ClusterService.RenameCluster or DriftService's mutations) — the mutation's
// own first line, not a second http-level wrapper.
func requireAdminRPC(ctx context.Context) error {
	if !userOf(ctx).IsAdmin() {
		return status.Error(codes.PermissionDenied, "This action requires an admin account.")
	}
	return nil
}
