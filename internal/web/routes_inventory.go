package web

import "net/http"

// The Inventory group: Nodes, Instances and Clusters — what is out there, at the
// three levels the product names things at.
func (s *Server) routesInventory(m *http.ServeMux) {
	// Sites is the SPA now (docs/adr/0017): GET /api/ui/sites and
	// GET /api/ui/sites/{name} (internal/api/sites.go).
	m.HandleFunc("GET /sites", s.auth(s.serveSPA))
	m.HandleFunc("GET /sites/{name}", s.auth(s.serveSPA))

	// Nodes are the addressable inventory unit. Every tab hangs off a node with
	// a process picker in the title bar. The /instances hierarchy is gone; its
	// URLs redirect here.
	m.HandleFunc("GET /nodes", s.auth(s.nodes))
	m.HandleFunc("GET /nodes/import", s.admin(s.importNodesPage))
	m.HandleFunc("POST /nodes/import", s.admin(s.importNodes))
	m.HandleFunc("POST /nodes", s.admin(s.addNode))
	m.HandleFunc("GET /nodes/{id}", s.auth(s.nodeDetail))
	m.HandleFunc("GET /nodes/{id}/{tab}", s.auth(s.nodeDetail))
	m.HandleFunc("POST /nodes/{id}/collect", s.admin(s.collectNode))
	m.HandleFunc("POST /nodes/{id}/credential", s.admin(s.changeNodeCredential))
	m.HandleFunc("POST /nodes/{id}/delete", s.admin(s.deleteNode))

	// /instances redirects to /nodes. The old /instances/{id} URLs redirect to
	// /nodes/{nodeID}?process={id} using the instance→node lookup.
	m.HandleFunc("GET /instances", moved("/nodes"))
	m.HandleFunc("GET /instances/{id}", s.auth(s.instanceRedirect))
	m.HandleFunc("GET /instances/{id}/{tab}", s.auth(s.instanceRedirect))

	// Clusters is the SPA now (docs/adr/0017): GET /api/ui/clusters and
	// POST /api/ui/clusters/rename (internal/api/clusters.go).
	m.HandleFunc("GET /clusters", s.auth(s.serveSPA))

	// /fleet was Nodes, Instances and Clusters on one screen.
	m.HandleFunc("GET /fleet", moved("/nodes"))
}
