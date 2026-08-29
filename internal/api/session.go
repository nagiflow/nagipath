package api

import (
	"net/http"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
)

// getSession is the SPA's bootstrap call: everything internal/web's
// renderStatus injects into every server-rendered page (nav.go's counts, the
// CSRF token, the license banner text) as one payload fetched once on load
// and after any 401. Shape is proto/nagipath/api/v1/session.proto's
// SessionResponse (docs/adr/0018) — buf generate produces both this
// package's pb.SessionResponse and ui/src/api/pb's matching TS type.
func (s *Server) getSession(w http.ResponseWriter, r *http.Request) {
	u := userOf(r)
	c, _ := r.Cookie(cookieName)
	counts := s.DB.NavCounts(r.Context())

	resp := &pb.SessionResponse{
		User: &pb.SessionUser{
			Id:       u.ID,
			Username: u.Username,
			Role:     u.Role,
		},
		CsrfToken:          csrfToken(c.Value),
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
		_, resp.LicenseNotice = s.LicenseStatus(r.Context())
	}

	writeProto(w, http.StatusOK, resp)
}
