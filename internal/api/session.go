package api

import "net/http"

type navCounts struct {
	Nodes        int `json:"nodes"`
	Sites        int `json:"sites"`
	Clusters     int `json:"clusters"`
	Drift        int `json:"drift"`
	Certificates int `json:"certificates"`
}

type sessionResponse struct {
	User struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
		Role     string `json:"role"`
	} `json:"user"`
	CSRFToken          string    `json:"csrf_token"`
	MustChangePassword bool      `json:"must_change_password"`
	NavCounts          navCounts `json:"nav_counts"`
	LicenseNotice      string    `json:"license_notice"`
}

// getSession is the SPA's bootstrap call: everything internal/web's
// renderStatus injects into every server-rendered page (nav.go's counts, the
// CSRF token, the license banner text) as one JSON payload fetched once on
// load and after any 401.
func (s *Server) getSession(w http.ResponseWriter, r *http.Request) {
	u := userOf(r)
	c, _ := r.Cookie(cookieName)

	var resp sessionResponse
	resp.User.ID = u.ID
	resp.User.Username = u.Username
	resp.User.Role = u.Role
	resp.CSRFToken = csrfToken(c.Value)
	resp.MustChangePassword = u.MustChangePassword

	counts := s.DB.NavCounts(r.Context())
	resp.NavCounts = navCounts{
		Nodes: counts.Nodes, Sites: counts.Sites, Clusters: counts.Clusters,
		Drift: counts.Drift, Certificates: counts.Certificates,
	}

	if s.LicenseStatus != nil {
		_, resp.LicenseNotice = s.LicenseStatus(r.Context())
	}

	writeJSON(w, http.StatusOK, resp)
}
