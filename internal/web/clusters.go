package web

import (
	"net/http"
	"strconv"
)

// Clusters are discovered from the configuration itself, so there is no form for
// creating one and none for membership. The name is the only editable part, and it
// is edited on the fleet, where the cluster is the level above the node.
func (s *Server) renameCluster(w http.ResponseWriter, r *http.Request) {
	ctx, u := r.Context(), userOf(r)
	id, _ := strconv.ParseInt(r.FormValue("cluster"), 10, 64)
	if err := s.DB.RenameCluster(ctx, id, r.FormValue("name"), &u.ID); err != nil {
		redirect(w, r, "/fleet", "", err.Error())
		return
	}
	redirect(w, r, "/fleet", "cluster renamed", "")
}
