# Porting a screen to the new shell

The contract every screen is rebuilt against. Read this before touching a
template. The visual source of truth is `design/nagipath Wireframes.dc.html`
(screens `2a`–`2w`); the functional source of truth is `docs/frontend/*.md`,
`docs/backend/*.md` and the code in `internal/store`.

**When the two disagree, the wireframe wins on layout and the code wins on
data.** The wireframe is a sketch drawn against a fictional 428-node fleet, so
it shows controls for features that do not exist. Render what the store can
answer. Never invent a field, never fake a number, never ship a control that
does nothing. A panel the backend cannot fill is a panel you leave out, and you
say so in your report.

The full list of sketched controls deliberately absent, so nobody re-adds one
believing it was an oversight:

| Area | Not built, because |
|---|---|
| Every screen | Share, Export ▾, Customize, Views, Saved queries, Save search — nothing persists a view, and a "saved trace" would be a stored result nobody can re-derive. |
| Settings | SAML, external KMS / Vault, per-user cluster scopes, a Service role, API-key scopes / rate limits / IP restriction / request logs. Roles are viewer and admin; the master key is a file on disk. |
| Credentials | Credential testing, "test on N nodes", bastion / jump-host type, an "assigned to" cluster scope, a health column. Credentials resolve per node and have no runtime state. |
| Collection | A schedule editor, bulk collection. One global interval, which the collector actually reads. |
| Host keys | Trust-on-first-use. Every key is decided by a person. |
| Certificates | Chain completeness, OCSP, an externally-reachable flag, "find replacements". The collector parses the leaf; `combined_pem` is the one bundle fact stored. |
| Drift | Reviewed / viewed tracking, "mark node reviewed", file-text diffs, patch export. Drift is computed over parsed objects, so a reformat is not a divergence. |
| Config files | The include tree, file mode / owner / mtime. Rebuilding the tree means reparsing every file per page load. |
| Trace | Historical traces, snapshot selection, "diff vs previous", headers, "candidates not chosen", graph zoom. A trace always walks the current snapshot. |
| Node › Routes | "Test a path", shadowed-route detection. Nothing computes shadowing. |

API keys, collection defaults, retention and the audit log **are** implemented —
they are in this list only where a specific control on them is not.

---

## 1. Stack

| Layer | What |
|---|---|
| HTML | Go `html/template`, one file per screen in `internal/web/templates/` |
| CSS | Tailwind v4. Tokens and components in `static/app.src.css`, compiled to the committed `static/app.css` |
| JS | htmx 2 (vendored) for filters, sorting, live progress; `static/app.js` for keyboard shortcuts. Nothing else. |

```
make css      # after ANY class change in a template — Tailwind scans templates/
make test     # go vet ./... && go test ./...
```

`static/app.css` is generated and committed. If you add a utility class to a
template and do not run `make css`, that class does not exist at runtime.

**No inline `<script>` and no inline event attribute anywhere.** CSP is
`script-src 'self'` with no `'unsafe-inline'`, so `onclick=`, `onsubmit=`,
`onchange="this.form.submit()"` and friends do not misbehave — they never run.
That failure is silent: the control renders, the click does nothing, and a
`confirm()` on a destructive form means the form submits on the first click.
`TestNoTemplateReliesOnInlineScript` fails the build on any of them. Instead:

| You wanted | Use |
|---|---|
| `confirm()` before a destructive POST | `<details class="cnf" data-panel>` — summary is the button, `.pop` holds the sentence and the real submit. Escape closes it. |
| a select that reloads on change | a real submit button in the `.qbar` |
| copy to clipboard | `data-copy="<selector>"`, bound by one delegated listener in `app.js` |
| auto-refresh while something runs | a self-terminating htmx poll (§6), never `<meta http-equiv="refresh">` |

No CDN, no web font link, no external image — an air-gapped install must render
identically to a connected one.

---

## 2. The shell

`layout.html` owns the header, the sidebar and the breadcrumb. A screen never
draws any of them. It gets the trail — `Inventory / Instances / app-nginx-042` —
for free from `nav.go`, using the section it is routed under and its own
`Title`. Do not add a breadcrumb.

The skeleton, in this order, and only this order:

```gotemplate
{{template "head" .}}
{{with .Data}}
{{template "ptitle" toolbar "Instances" "1,164 on 428 nodes"}}
  <a class="btn sm" href="/instances?export=csv">Export</a>
  <button class="btn p sm" type="submit" form="collect">Collect now</button>
{{template "ptitle-end"}}

{{/* optional: one .tabs strip, then one .qbar */}}
<div class="qbar">
  <input class="fld f m" type="search" name="q" value="{{.Query}}" placeholder="filter by name, vendor or node">
  <select class="sel" name="vendor">…</select>
  <button class="btn p" type="submit">Filter</button>
</div>

{{template "bd"}}
  <div class="pnl z">
    <div class="phd"><span class="ph2">Instances</span><span class="m mus">{{num .Total}}</span></div>
    <table class="t">…</table>
  </div>
{{template "bd-end"}}
{{end}}
{{template "foot" .}}
```

- `{{template "bd" "row"}}` for a two-column screen (`flex-row`).
- Only `.bd` scrolls. `.ptitle`, `.tabs` and `.qbar` are `flex: none`.
- `{{with .Data}}` shadows the dot, so the page struct is `$`: `$.CSRF`, `$.User.IsAdmin`.

---

## 3. Component classes

Defined in `static/app.src.css`. These are the vocabulary — reach for a Tailwind
utility only for one-off geometry (a panel width, a column span), which is what
the wireframe's inline `style=""` attributes become.

| Class | Use |
|---|---|
| `.pnl` / `.pnl.z` | Panel. `.z` for one whose child owns its edges (a table, a scroll region) — pair with `.phd` / `.pft`. |
| `.phd` `.ph2` `.pft` | Panel header, its title, panel footer. |
| `.row` `.col` | Flex row / column, 10px gap. |
| `.t` | Data table. `th` is sticky and uppercase already; declare widths. |
| `.t tr.hl` / `.t tr.zz` | The selected row / a continuation row belonging to the one above. |
| `.m` `.mu` `.mus` | Machine text (mono), then one and two steps back in ink. |
| `.lbl` | Uppercase mono micro-label. A section key or a stat caption — never a value. |
| `.bdg` + `ok` `deg` `err` `inf` `ext` `note` `none` | Badge. Always via `{{template "confidence" …}}` when it carries a verdict. |
| `.btn` + `p` `s` `dgr` `sm` | Button (primary, secondary, destructive, small). Works on `<button>` and `<a>`. |
| `.link` | A `<button>` that reads as text. |
| `.sel` | `<select>`. Real select — `appearance:none` plus the caret is already handled. |
| `.fld` / `.fld.f` | Input, or a wrapper around one with prefixes. `.f` to fill the row. |
| `.chip` | A removable filter, above the table. |
| `.stat` | One figure in the row a screen leads with — via `{{template "stats" …}}`. Caption above figure above sub-line, and a warned tile tints all three: on a row of six, the label is what the eye lands on first. |
| `.tabs` `.tab` | Tab strip — via `{{template "tabs" …}}` with `[]navItem`. |
| `.code` `.cl` `.cl.on` `.no` | Config viewer: line, highlighted byte range, line number. |
| `.ev` | Verbatim evidence — a log line, a response header. Never reformatted. |
| `.node` `.node.on` `.node.ext` `.edge` `.canvas` | Path graph. |
| `.kv` | Two-column definition grid. |
| `.empty` | Empty state — via `{{template "empty" …}}`. |
| `.pg` `.pgb` | Pagination. |
| `.bar` | A proportion bar (`<div class="bar"><i style="width:42%"></i></div>`). |
| `details.cnf` + `.pop` | Two-step confirmation for a destructive action. The only confirm this product has. |
| `.solo` | The signed-out card: `login.html`, `setup.html`. Those pages get a bare `<main>`, not the shell. |

**Wireframe class → ours.** The design doc uses shorter names in a few places:

| Wireframe | Here |
|---|---|
| `.bg` + `.v .i .d .e .r .n` | `.bdg` + `.ok .inf .deg .ext .err .note` |
| `<span class="btn">` | a real `<button class="btn">` or `<a class="btn">` |
| `<span class="sel">` | a real `<select class="sel">` (or `.sel.caret` if it is genuinely not a control) |
| `<span class="cb">` / `.rd` | `<input type="checkbox" class="cb">` / `<input type="radio" class="rd">` |
| `<div class="ni">` in a panel | `{{template "sidelist" …}}` with `[]navItem` |
| `style="flex:1"` spacer | `<div class="flex-1"></div>` — already emitted by `ptitle` |

---

## 4. Partials

| Call | Notes |
|---|---|
| `{{template "ptitle" toolbar "Title" "caption"}}` … `{{template "ptitle-end"}}` | Open/close. Emits the flex spacer, so actions between them are right-aligned. |
| `{{template "bd"}}` / `{{template "bd" "row"}}` … `{{template "bd-end"}}` | The scrolling column. |
| `{{template "cols" cols "instance" "vendor:6rem" ":2rem"}}` | Heading row; `label:width`. |
| `{{template "stats" stats (stat .Nodes "nodes" "/nodes" "") (statif (gt .Pending 0) .Pending "host keys to approve" "/settings/hostkeys" "warn")}}` | `statif` drops a stat with nothing to report. |
| `stat … \| note "88 bindings"` | Fills the tile's sub-line. Takes the `Stat` last so it reads as a pipeline. A figure whose unit is ambiguous — "9" over "≤ 30 days" — says which nine here. |
| `{{template "confidence" .Confidence}}` | Tone, word and hover reason, all three. |
| `{{template "terminal" .TerminalReason}}` | Why a walk stopped. |
| `{{template "prov" .Rule}}` | `path:offset ↗`, linked to the byte range. |
| `{{template "rulelink" .Rule}}` | The directive itself as the link. |
| `{{template "tabs" .Tabs}}` / `{{template "sidelist" .Sections}}` | Both take `[]navItem`. |
| `{{template "empty" "No instances yet"}}` … `{{template "empty-end"}}` | Open/close so the prose stays prose. |
| `{{template "csrf" $.CSRF}}` | Every state-changing form. The middleware rejects the request without it. |

Template funcs worth knowing: `num` (1,164), `bytes`, `label`, `why`, `bdg`,
`join`, `csv`, `expiry`, `hl` (search highlight), `add`, `sub`.

---

## 5. Rules that are not style preferences

1. **Provenance on every derived fact.** Every Rule, Route, Site, Listener,
   Upstream, Certificate Binding and Drift finding links to the file and byte
   range it was parsed from. A screen showing a derived fact with no way to
   check it is an incomplete screen.
2. **Every state is a word.** Never a bare dot or colour. `{{template
   "confidence"}}` handles the tone, the word and the hover reason together.
   Aggregates show the worst of their children, never an average.
3. **Freshness.** Any view of derived data says when it was collected. In a
   table cell that is `{{clock .At}}` — `04:12Z`, always UTC, always stamped
   `Z`, dated once it is not today. A relative "3h ago" cannot be compared
   against a log line or read out on a call, and every one of these cells is
   read next to a timestamp from somewhere else.
4. **Empty states name the fix.** Distinguish *nothing collected yet* from
   *filtered to nothing* from *the collection failed*. Three different
   situations; one generic "No results" conflates them.
5. **Filters live in the URL.** Every filtered view must be a link an operator
   can paste into a ticket. `hx-push-url="true"` on htmx filter requests.
6. **Semantic HTML.** Tables are tables, buttons are buttons, a row that
   navigates is an `<a>`. Every form control has a label (`.sr-only` if the
   design shows none). Tested keyboard-only.
7. **Viewers read, admins write.** Never render an action a viewer cannot
   perform — drop it, do not disable it.
8. **Fleet errors are not tool errors.** A Probe that fails because *we* could
   not reach the entry point says exactly that. The customer's fleet may be
   perfectly healthy.

## 6. htmx conventions

- `hx-get` for filters, sorting, pagination, detail panels; `hx-push-url="true"`.
- `hx-post` for actions, returning a fragment plus an out-of-band swap for any counter it changed.
- `hx-trigger="every 3s"` for live progress, **and the poll stops when the operation finishes.**
- `hx-indicator` on anything over ~200ms; the target carries `.spin`.
- Every screen must work with JavaScript disabled, except the path graph's
  pan/zoom. A filter form is a real `<form method="get">` that htmx enhances —
  not a div with `hx-get` on it.
