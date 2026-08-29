package web

import (
	"io"
	"io/fs"
	"net/http"
)

// spaDist is spaAssets rooted at ui/dist, so a request for "/assets/x.js"
// resolves directly against the embedded "ui/dist/assets/x.js" file — the
// same relationship /static/ has to the assets embed.FS.
var spaDist = func() fs.FS {
	d, err := fs.Sub(spaAssets, "ui/dist")
	if err != nil {
		// spaAssets is compiled in via go:embed; a bad path here is a build-time
		// bug, not a runtime condition to recover from.
		panic(err)
	}
	return d
}()

// serveSPA serves the built React app's shell for a route the SPA now owns.
// Every ported route (see routes()) points here; React Router decides what
// to render client-side based on the URL. Not yet the catch-all — during the
// phased migration (docs/adr/0017) unported paths still go to their Go
// handler, so this is registered per-route rather than at "/".
func (s *Server) serveSPA(w http.ResponseWriter, r *http.Request) {
	f, err := spaDist.Open("index.html")
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := io.Copy(w, f); err != nil {
		s.Log.Warn("serveSPA", "err", err)
	}
}

// registerSPAAssets mounts the SPA's static files at the fixed paths Vite's
// build emits: the hashed JS/CSS bundle under /assets/, plus the two
// root-level icon files main.tsx's index.html references directly.
func (s *Server) registerSPAAssets(m *http.ServeMux) {
	m.Handle("GET /assets/", http.FileServerFS(spaDist))
	m.Handle("GET /favicon.svg", http.FileServerFS(spaDist))
	m.Handle("GET /icons.svg", http.FileServerFS(spaDist))
}
