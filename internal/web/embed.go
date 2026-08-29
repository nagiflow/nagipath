package web

import "embed"

// spaAssets is the built React SPA (ui/, built by `make ui`), committed to
// ui/dist the same way static/app.css is generated-and-committed (ADR-0016,
// extended by ADR-0017): `go build`, `go install` and an air-gapped clone all
// need Go and nothing else. Not yet served — wired up starting Phase 1 as
// pages are ported, page group by page group (docs/adr/0017).
//
//go:embed ui/dist
var spaAssets embed.FS
