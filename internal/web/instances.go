package web

import (
	"fmt"
	"net/http"

	"github.com/nagiflow/nagipath/internal/store"
)

// The fleet-wide /instances list and the per-instance detail page that used to
// live here are gone: a node is the addressable unit now, and every tab they had
// hangs off /nodes/{id} with a process picker in the title bar. Snapshot history
// is its own screen at /snapshots. What is left is the redirect that keeps old
// links working.

type fileData struct {
	Snapshot store.Snapshot
	// Instance is what the title bar names the file's snapshot by. The template
	// read .Snapshot.DisplayName, a field store.Snapshot does not have, so this
	// page — the far end of every provenance link in the product — was a 500 for
	// every file an operator tried to check.
	Instance  store.Instance
	File      store.FileRef
	Body      string
	ByteStart int // the byte offset a provenance link points at
	LineStart int // the 1-based line number that byte offset falls on
	LineEnd   int // the ending line for multi-line highlights
	HasAnchor bool
}

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
