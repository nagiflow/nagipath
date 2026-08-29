package web

import (
	"net/http"
	"strings"
)

// backTo is where a form that can be submitted from more than one screen returns
// to. It only ever yields a path on this server: "//evil.example" starts with a
// slash but a browser reads it as a host, so a bare HasPrefix("/") check is an
// open redirect.
func backTo(r *http.Request, def string) string {
	back := r.FormValue("back")
	if !strings.HasPrefix(back, "/") || strings.HasPrefix(back, "//") ||
		strings.Contains(back, "\\") {
		return def
	}
	return back
}

// decideHostKey is /settings/hostkeys' approve/reject action — the node
// detail page's own inline decision now posts to
// POST /api/ui/hostkeys/{id}/decide (internal/api/nodes.go) instead, but
// this server-rendered page isn't ported until Phase 4.
func (s *Server) decideHostKey(w http.ResponseWriter, r *http.Request) {
	ctx, u := r.Context(), userOf(r)
	id := idOf(r, "id")
	approve := r.FormValue("decision") == "approve"
	if err := s.DB.DecideHostKey(ctx, id, approve, &u.ID); err != nil {
		redirect(w, r, "/nodes", "", err.Error())
		return
	}
	back := backTo(r, "/nodes")
	if approve {
		redirect(w, r, back, "host key approved", "")
		return
	}
	redirect(w, r, back, "host key rejected", "")
}
