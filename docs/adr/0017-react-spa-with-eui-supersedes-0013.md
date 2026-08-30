# The UI is a React SPA served from an embedded, committed build — not server-rendered templates

[ADR-0013](0013-server-rendered-ui.md) rejected a React/Vue SPA: "the requirement is filterable
tables that render fast and a graph that loads, and a second toolchain, build step and frontend
skillset is disproportionate cost for a solo part-time build that is simultaneously writing three
configuration parsers." That was right at the time. It stopped being right once the UI grew to
around twenty resources and thirty-four screens: `internal/web/funcs.go` reached 961 lines of
business logic smuggled into a template `FuncMap` because templates needed it somewhere, and the
664-line CSS file was outnumbered by hundreds of one-off `style="..."` attributes carrying the
inconsistency those numbers predict. ADR-0013 itself named this outcome: "a future contributor will
assume an SPA was an oversight and offer to 'fix' it... that trade was made knowingly." This
reverses that trade, deliberately, now that the product needs UI consistency more than it needs a
one-toolchain build.

## Decision

The UI is React + TypeScript + Elastic UI (EUI), built with Vite. The build output is **committed**
to `internal/web/ui/dist/` and embedded via `go:embed`, extending [ADR-0016](0016-tailwind-build-step-with-generated-css-committed.md)'s
precedent (committed `app.css`) to the whole UI rather than reopening it: `go build`, `go install`
and an air-gapped clone still need nothing but Go. `make ui` (bun) regenerates it; CI fails if the
committed output has drifted from source.

Business logic that lived in `internal/web/funcs.go`'s template functions moves to a new
`internal/api` package as computed JSON response fields — never reimplemented in TypeScript.
`internal/web` is demoted to serving the embedded SPA and mounting `internal/api`'s mux at
`/api/v1/`; every page, including login/setup/password/404 (Phase 8) and Import inventory
(Phase 9, the last holdout), is ported to it. `internal/web` renders no HTML of its own — the
`html/template` engine, `templates/`, `static/` and the Tailwind CLI toolchain are gone entirely.

Auth stays a session cookie (`nagipath_session`, HttpOnly) with the existing stateless CSRF token
(`X-CSRF-Token` header, derived from the session, no server-side store) — a same-origin single
binary removes the only reason to prefer a bearer token in JS, and HttpOnly keeps the session id
out of reach of any XSS in the bundle.

## What does not change

Still one binary. Still no Node/npm required to `go build`, `go install`, or produce a working
binary from a network-less clone — only *editing* the UI requires `bun`. Still SQLite, still no
external services. Still the same two-role auth model and CSRF mechanism.

## Consequences

**Editing the UI now requires `bun` locally**, unlike ADR-0016's standalone-CLI approach: a React
app's dependency graph cannot be hand-rolled as one downloaded binary the way a single CSS compiler
could. `ui/` gets a real `package.json` and lockfile.

**`internal/web/ui/dist` is a large generated diff** on every UI change — hundreds of files,
including EUI's per-icon lazy chunks. Same discipline as `app.css`: review `ui/src`, treat
`ui/dist` as an artifact, let CI's drift check catch a forgotten rebuild.

**The Go template/htmx/`app.js` code and the Tailwind CLI toolchain are deleted, not kept as a
fallback** — there is no backward-compatibility requirement, so each page group is deleted from
`internal/web` in the same change that ports it to React, rather than the two implementations
coexisting.

## Rejected alternatives

- **More htmx.** Does not solve the two problems that actually motivated this: business logic
  trapped in a template `FuncMap`, and no component reuse across thirty-four hand-written screens.
- **Vue or Svelte instead of React.** No technical reason; EUI (Kibana's design system) is a React
  library, and React + EUI was the explicit requirement.
- **Building the SPA fresh in CI instead of committing `dist`.** Breaks the single-binary,
  no-toolchain-to-build property this ADR chain exists to protect, and would require Node in every
  release pipeline permanently — the same reason ADR-0016 chose a standalone CLI over an npm
  package, at higher stakes since this covers the whole UI, not one stylesheet.
