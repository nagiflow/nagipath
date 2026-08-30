package api

import (
	"context"
	"net/http"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
	"github.com/nagiflow/nagipath/internal/store"
)

// getSession is the SPA's bootstrap call: everything internal/web's
// renderStatus used to inject into every server-rendered page (nav.go's
// counts, the CSRF token, the license banner text) as one payload fetched
// once on load and after any 401. Shape is proto/nagipath/api/v1/session.proto's
// SessionResponse (docs/adr/0018) — buf generate produces both this
// package's pb.SessionResponse and ui/src/api/pb's matching TS type.
//
// Stays a plain http.HandlerFunc rather than an RPC on SessionService — see
// session.proto's SessionService doc comment for why (it needs to peek at
// the cookie, not require it, which doesn't fit requireAuth's shape).
//
// Public, deliberately: an anonymous caller (no cookie, or one SessionUser
// rejects) gets a 200 with authenticated:false and setup_required rather than
// a 401 — the Login and Setup pages need to know which of themselves to
// render before any session exists at all, and can't do that from an error.
func (s *Server) getSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c, err := r.Cookie(cookieName)
	if err != nil {
		s.writeAnonymousSession(w, r)
		return
	}
	u, err := s.DB.SessionUser(ctx, c.Value)
	if err != nil {
		s.writeAnonymousSession(w, r)
		return
	}
	writeProto(w, http.StatusOK, s.sessionResponse(ctx, u, c.Value))
}

func (s *Server) writeAnonymousSession(w http.ResponseWriter, r *http.Request) {
	n, _ := s.DB.UserCount(r.Context())
	writeProto(w, http.StatusOK, &pb.SessionResponse{SetupRequired: n == 0, DemoMode: s.DemoMode})
}

// sessionResponse builds the authenticated session payload, shared by
// getSession and Setup/Login/ChangePassword (sessionservice.go) — each of
// those returns it directly after establishing a new session, so the client
// gets the fresh CSRF token and user state without a second round trip.
func (s *Server) sessionResponse(ctx context.Context, u store.User, sessionToken string) *pb.SessionResponse {
	counts := s.DB.NavCounts(ctx)
	resp := &pb.SessionResponse{
		Authenticated: true,
		User: &pb.SessionUser{
			Id:       u.ID,
			Username: u.Username,
			Role:     u.Role,
		},
		CsrfToken:          csrfToken(sessionToken),
		MustChangePassword: u.MustChangePassword,
		DemoMode:           s.DemoMode,
		NavCounts: &pb.NavCounts{
			Nodes:        int32(counts.Nodes),
			Sites:        int32(counts.Sites),
			Clusters:     int32(counts.Clusters),
			Drift:        int32(counts.Drift),
			Certificates: int32(counts.Certificates),
		},
	}
	if s.LicenseStatus != nil {
		_, resp.LicenseNotice = s.LicenseStatus(ctx)
	}
	return resp
}
