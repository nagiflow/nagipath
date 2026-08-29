package web

import (
	"fmt"
	"net/http"
)

// The fleet-wide /instances list and the per-instance detail page that used to
// live here are gone: a node is the addressable unit now, and every tab they had
// hangs off /nodes/{id} with a process picker in the title bar. Snapshot history
// is its own screen at /snapshots. What is left is the redirect that keeps old
// links working.

// instanceRedirect redirects /instances/{id} to /nodes/{nodeID}?process={id}.
// This exists so old links do not 404 after the design switched to nodes as the
// addressable unit.
func (s *Server) instanceRedirect(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	instanceID := idOf(r, "id")
	inst, err := s.DB.Instance(ctx, instanceID)
	if err != nil {
		s.notFound(w, r)
		return
	}
	tab := r.PathValue("tab")
	dest := fmt.Sprintf("/nodes/%d", inst.NodeID)
	if tab != "" {
		dest += "/" + tab
	}
	dest += fmt.Sprintf("?process=%d", instanceID)
	http.Redirect(w, r, dest, http.StatusMovedPermanently)
}
