# The CSS is built by Tailwind, and the generated file is committed

`internal/web/static/app.css` is generated from `app.src.css` by the standalone Tailwind v4 CLI (`make css`) and **committed to the repository**. It is embedded through the same `embed.FS` as everything else, so `go build`, `go install` and a clone with no network all produce a working binary with no toolchain beyond Go.

This reverses the earlier position that the frontend has no build step. That position was right when the CSS was 400 hand-written lines; it stopped being right at the point where a 23-screen design system had to stay internally consistent across six people editing templates in parallel. A design token changed by hand in one place and missed in three others is the failure mode the build step removes.

The standalone CLI was chosen over the npm package specifically to keep the dependency surface at *one downloaded binary*, not a `node_modules` tree. There is no `package.json`, no `node`, no lockfile, and nothing in CI that resolves a registry. `make css` downloads a single pinned release into `tools/`, which is gitignored.

## Consequences

**Editing a template's class names, or the theme, requires the CLI.** Tailwind scans `templates/` to decide which utilities to emit, so a utility class added to a template does not exist at runtime until `make css` runs. This is the real cost, and it is a trap for a contributor who does not know it: the page renders, silently unstyled in one corner. It is stated at the top of `app.src.css`, in `docs/frontend/porting.md`, and here.

**Building the product does not require the CLI.** This is the whole point of committing the output. A contributor fixing a Go bug, a user running `go install`, and an air-gapped build all need Go and nothing else. `make css` failing on an unsupported platform is a warning, not a build failure.

**The generated file is a real diff.** `app.css` shows up in review as thousands of changed lines whenever the theme moves. Review `app.src.css` and ignore `app.css`; it is an artifact, and treating it as source is how it eventually gets hand-edited and then silently overwritten.

This does not reopen [ADR-0013](0013-server-rendered-ui.md). There is still no JavaScript framework, no bundler, no transpiler and no client-side router — the UI is still server-rendered Go templates. What changed is that one stylesheet is generated instead of typed.
