# Design System

**Decisions:** ADR-0005 (server-rendered `html/template` + htmx; JS only for the Trace graph).
Every other document in this directory assumes what is defined here and does not restate it.

---

## 1. Constraints that shape everything

| Constraint | Consequence |
|---|---|
| Single binary, UI in `embed.FS` | No build step, no `node_modules`, no CDN. Every asset is committed and embedded. |
| Server-rendered, htmx for interactivity | State lives on the server. There is no client store to desynchronise. |
| Air-gapped installs are a target | **No external fonts, no CDN, no analytics, no telemetry.** Every byte ships in the binary. |
| One part-time engineer | The component set is small and reused. A bespoke component is a maintenance liability. |
| Operators, often on a bad VPN over a terminal-adjacent workflow | Fast first paint, works at 1280×720, keyboard-first, degrades without JS except the graph. |

The JS budget is roughly 400 lines plus Cytoscape.js, all committed. If a feature needs more, the feature is wrong for this product.

---

## 2. Visual language

**An observability console, in Elastic UI's idiom.** The audience reads configuration files for a living, works next to Kibana and Grafana all day, and wants density with high contrast. So the page is EUI: flat white panels on a cool grey page, one blue that means *link or primary action*, and every state carried by a soft filled badge that also spells the word. Panels, not paper. No serif, no italics, and nothing hanging off a hairline rule in the margin — that earlier "case file" direction is what made these screens hard to read.

Two tells that break the idiom, both absent by policy: uppercase letter-spaced monospace micro-labels (console cosplay), and saturated fills behind small text (EUI tints at ~10% and pairs them with a darkened text-safe shade).

`internal/web/static/app.css` is the entire theme, and class names are stable — a restyle is that file and nothing else.

### 2.1 Type

Two roles from system stacks. No web fonts and no CDN: an air-gapped install with a missing font is a broken install. `--sans` names Inter first, so a machine that has Inter renders exactly EUI and every other machine falls back to its native UI font, which is close enough. (If pixel-identical Inter matters, commit a subset woff2 into `static/` — embedded, still no CDN — rather than adding a link tag.)

| Role | Token | Used for |
|---|---|---|
| UI | `--sans` (Inter, `-apple-system`, Segoe UI Variable, Roboto) | Everything: headings, prose, controls, table cells, labels, badges |
| Machine | `--mono` (Roboto Mono, `ui-monospace`, SFMono-Regular, Menlo) | **Only literal machine text:** config, paths, directives, hostnames, fingerprints, ports, log lines |

`--mono` is not a stylistic option. A path in a proportional font is harder to trust; a *label* in monospace is console cosplay.

Scale 26 / 20 / 17 / 15 / 14 / 13 / 12. Body 14px at line height 1.45, tables 13px, code 12.5px at 1.6. Headings are 700 with `-.02em` tracking; labels (`.lbl`, `h4`, `dt`, form labels) are 12–13px at 600 in heading ink, not subdued italics. `tabular-nums` wherever numbers are compared down a column.

Sentence case comes from `::first-letter` on `th`, `.lbl` and `.card span`, so templates keep writing labels lowercase.

### 2.2 Colour

Semantic only, EUI's palette. Nothing is coloured for decoration; a fill is reserved for badges, callouts, buttons and the selected nav item.

| Token | Light | Use |
|---|---|---|
| `--bg-page` | `#f7f8fc` | The page: cool grey |
| `--bg` | `#ffffff` | Panels, nav, page header, table header, inputs |
| `--bg-subtle` | `#f5f7fa` | Row hover, code blocks inside a panel |
| `--bg-sunken` | `#eef1f7` | Recessed on the page itself: the graph canvas, `.bx.dash` asides |
| `--border` / `--border-light` / `--border-strong` | `#d3dae6` / `#e9edf5` | Panel edges; table row rules; control edges on hover |
| `--fg` / `--fg-strong` / `--fg-muted` | `#343741` / `#1a1c21` / `#646a77` | Body; headings and labels; secondary text |
| `--accent` / `--accent-soft` | `#0b64dd` / `#e6effc` | Links, primary buttons, focus, selected nav and row |
| `--ok` / `--ok-soft` | `#067a63` / `#e2f4ef` | Verified, healthy, conforming |
| `--warn` / `--warn-soft` | `#7c5c00` / `#fcf3d6` | Partial, degraded, expiring, stale |
| `--danger` / `--danger-soft` | `#a71627` / `#fbe9e9` | Failed, expired, quarantined, denied |
| `--info` / `--info-soft` | `#5a6673` / `#eef1f6` | Inferred, candidate, informational |

Each status hue ships as a pair: the `-soft` tint is the only thing that ever sits behind text, and the solid shade is already darkened to clear AA on it.

Spacing is one scale (`--s1` 4px … `--s7` 48px), two radii (`--rad` 6px, `--rad-s` 4px) and two elevations (`--sh-s` for a panel on the page, `--sh-m` for hover and the login card). `--r` and `--l` are reserved: templates set them inline as `.split` column widths.

Links are blue and unadorned until hovered, EUI-style: in a 200-row table the colour is the affordance and an underline in every cell is the noise. Components that happen to be anchors (`.nv`, `.card`, `.chip`, `.gc`, `.tabs a`, `.button`, `.crumb`) opt out and inherit body ink.

Light only, `color-scheme: light`. A dark set is welcome behind an explicit user toggle; it is **not** wired to `prefers-color-scheme`, which is how operators on dark systems got an unreadable trace graph they never asked for.

Motion: nothing on load — this tool is opened mid-incident — 120ms on hover and focus only, and `prefers-reduced-motion` disables all of it.

**Colour is never the only carrier of meaning.** Every state has a text label as well. Confidence in particular is always a word — `Inferred`, `Verified` — because a coloured dot alone communicates nothing to a colourblind user and, more importantly, communicates nothing to anyone who has not learned the legend.

---

## 3. The confidence badge

The most important component in the product, because the product's credibility depends on it being unmissable.

| Badge | Meaning | Colour |
|---|---|---|
| `Inferred` | From configuration alone | info |
| `Candidate` | Could apply; no confirmation it did | info |
| `Observed effect` | Result visible in a Probe response | warn |
| `Verified` | Confirmed in the Vendor's own access log | ok |
| `Partial` | Mixed across children | warn |
| `Disproved` | Configured but proven not in effect | danger |

Rendered as EUI badges: the hue's `-soft` tint with its text-safe ink, 12px at 600, 4px radius. Hollow (white, `--border`) is the neutral, used for a terminal reason — where the walk stopped is a fact, not an alarm.

Rules:

1. **Always paired with a word.** Never a bare dot or colour.
2. **Hover or focus reveals the reason** — "no access log evidence: log_format lacks $server_name". A badge that does not explain itself trains people to ignore it.
3. **`Verified` and `Observed effect` link to their evidence** — the verbatim log line or response header. A claim of verified the operator cannot audit is worthless.
4. **Aggregates show the worst of their children**, never an average and never rounded up.

The temptation in every design review will be to make the badges smaller or move them to a tooltip. Resist it: the labelling is not chrome around the answer, it *is* part of the answer.

---

## 4. The provenance link

Second most important. Every derived fact shows where it came from:

```
/etc/nginx/conf.d/api.conf:27
```

Monospace, clickable, opens the Snapshot file viewer scrolled to and highlighting that byte range. Present on every Rule, Route, Site, Listener, Upstream, Certificate Binding and Drift finding.

This is the trust mechanism. An operator who does not believe an answer clicks through to the line and sees the directive. One click from claim to evidence, everywhere, without exception — a screen that shows a derived fact with no way to check it is an incomplete screen.

---

## 5. Component inventory

Small on purpose. Everything is built from these.

**Layout** — App shell with left nav, breadcrumb bar, content column, optional right detail panel. Full width; no artificial max-width, because these tables want the pixels.

**Data table** — Sortable headers, sticky header, per-column filters in a single row beneath, cursor pagination ("Load more", never page numbers — a Collection writing rows makes offset pagination skip and repeat). Row click opens detail. Column visibility toggles persisted in a cookie.

**Filter bar** — Filters as removable chips above the table, so the active query is always visible and one click undoes it. Encoded in the URL, so any filtered view is shareable — an operator pasting a link into a ticket is a core workflow.

**Detail panel** — Slides in from the right, `Esc` closes, URL updates so it is linkable and back-button-correct.

**Empty state** — Never a bare "No results". Always: what would be here, why it is not, and the action that fixes it. Distinguishes *nothing configured yet* from *filtered to nothing* from *collection failed* — three very different situations that a generic empty state conflates.

**Freshness stamp** — `Collected 2h ago` with an absolute time on hover, on every view of derived data. Non-negotiable, for the same reason as the confidence badge.

**Path graph** (`.gcol`) — Hop cards by rank (`.grank`), not in a line: a trace is a tree, and the members of one upstream are alternatives a balancer picks between at request time. A rank with more than one hop is bracketed (`.grank.fan`) and says so in words; the ↓ appears between ranks only. Cards lead with the node name, because two members of an upstream run the same vendor with the same config filename.

**Hop panel** (`.hop`) — One per hop on a Trace, read top to bottom in the order the server decides: the request it received, the listener, the site that claimed the hostname, the route that matched, any rewrite, then a tinted `.trans` block with the URL it calls next and the Host header it keeps. Rules sit under that, collapsed per config file (`<details class="rf">`, opened when the selected rule is inside), because a haproxy frontend is sixty directives and only a handful routed. Global-scope directives are counted and linked, never listed: they apply to every hop equally, so they decide nothing about this one.

**Evidence block** — Monospace, scrollable, verbatim. Used for log lines, response headers and configuration excerpts. Never reformatted or prettified; the bytes as they were.

**Code block with line numbers** — Configuration display, with byte-range highlighting and a copy button.

**Diff view** — Side-by-side above 1400px, unified below.

**Banner** — Page-top notices: licence state, quarantined Nodes, degraded Collections, demo mode. Dismissible ones remember the dismissal per user; licence and demo banners are not dismissible.

**Toast** — Transient confirmations, bottom-right, 5 seconds, `aria-live="polite"`. Every action result is one (`.flash.toast`, `role=status`; errors keep `role=alert` and do not self-dismiss): a strip above the page pushes the table down at the moment the operator is reading it. The dismissal is a CSS animation, so no script is involved.

**Confirm dialog** — For destructive or outward-facing actions. **Names the specific consequence**, not "Are you sure?" — "Purge web12 and delete 34 Snapshots, 1,890 stored files and 12 Traces. This cannot be undone." Typed confirmation for purge and for host key re-approval.

**Progress list** — Per-item live status for bulk operations, polled by htmx. Shows individual failures as they happen rather than one summary at the end.

### 5.1 Where the shared ones live

One component per file under `internal/web/templates/parts/`, each a single `{{define}}`. A partial takes one argument, so anything with more than one field has a constructor in `internal/web/parts.go` and is built in the template.

| Partial | Call | Notes |
|---|---|---|
| `toolbar` / `body` | `{{template "toolbar" toolbar "Fleet" "clusters, nodes and the instances found on them"}}` … `{{template "body"}}` | An open/close pair like `head`/`foot`: the page's own actions go between them. |
| `cards` | `{{template "cards" cards (card .Total "nodes" "/nodes" "") (card .Pending "host keys" "/onboarding" (warnif .Pending))}}` | Tone is `""`, `warn` or `dashed`. `cardif` omits a card whose condition is false, so no screen leads with "0 to approve". A card with a href is a way in; one without is a fact. |
| `cols` | `{{template "cols" cols "node" "vendor:6rem" ":1.4rem"}}` | `label:width`. Widths are declared so a column does not resize between page loads. |
| `csrf` | `{{template "csrf" $.CSRF}}` | Every state-changing form; the middleware rejects the request without it. |
| `empty` / `empty-end` | `{{template "empty" "No nodes yet"}}` … `{{template "empty-end"}}` | Open/close so the prose stays prose — an empty state's sentence usually carries a link, which reads better inline than passed in as a string. |
| `prov`, `confidence`, `terminal` | `{{template "prov" .Rule}}` | The three that carry a verdict rather than a label. |

Nine screens keep a hand-written toolbar: `drift`, `rules`, `search` and `trace` put a query form in it, and `file`, `instance`, `node`, `password` and `probe` put breadcrumb links or badges beside the title. Those belong to the page, and a partial that took them as parameters would need a hook per screen — which is the layout inheritance this codebase deliberately does without.

---

## 6. Interaction patterns

### 6.1 htmx conventions

- `hx-get` for filters, sorting, pagination and detail panels. `hx-push-url="true"` so everything is linkable.
- `hx-post` for actions, returning a fragment plus an out-of-band swap for any affected counter.
- `hx-trigger="every 3s"` for live progress, **and the polling stops when the operation completes** — a page that polls forever is a page that keeps a database connection busy forever.
- `hx-indicator` on every request over ~200ms.
- `hx-confirm` for reversible actions; a real dialog for irreversible ones.

### 6.2 Loading

Three tiers, matched to duration:

| Duration | Treatment |
|---|---|
| < 200ms | Nothing. A flashed spinner is worse than a brief wait. |
| 200ms – 2s | Inline spinner on the triggering control; the control disables to prevent double submission. |
| > 2s | Skeleton rows for tables; for Traces, a staged progress message ("resolving listeners… selecting sites… walking upstreams") because a five-second blank panel reads as broken. |

Long operations (Collection, bulk Collection, Probe) return `202` immediately and render into a progress list. The page is never blocked on them.

### 6.3 Errors

Every error shows: **what failed, why, and what to do next.** The API's stable error `code` maps to a message; unmapped codes fall back to the server's message rather than to "Something went wrong", which is the single least useful string in software.

| Class | Treatment |
|---|---|
| Validation (`422`) | Inline, on the field, with the specific constraint. Form state preserved. |
| Conflict (`409`) | Inline, naming the conflicting object with a link to it. |
| Denied (`403`) | Plain statement of which role is required. No retry button — retrying will not help. |
| Not found (`404`) | Page-level, offering the list view. |
| Server (`5xx`) | Page-level with the request ID for the log. |
| Transport (htmx failure) | Toast with a retry that re-issues the original request. |

**Fleet errors are never presented as tool errors, and tool errors are never presented as fleet findings.** A Probe that fails because *we* cannot reach the Entry Point says so — the customer's fleet may be perfectly healthy, and blaming it would be both wrong and embarrassing.

### 6.4 Keyboard

`/` focuses search. `g` then `t`/`f`/`d`/`c` jumps to Trace, Fleet, Drift, Certificates. `Esc` closes panels and dialogs. `?` shows the shortcut sheet. Full tab order, visible focus rings, `Enter` activates the focused row.

This audience lives on keyboards. Shortcuts are not a power-user extra here; they are the expected interface.

---

## 7. Accessibility

Not a compliance exercise — this software is sold into government and finance, where it is procurement-blocking.

- Semantic HTML. Tables are tables, buttons are buttons. htmx makes it easy to attach behaviour to a `div`; the codebase does not.
- WCAG AA contrast on all text including badges.
- Every state carries a text label; colour never carries meaning alone.
- Visible focus indicators, never removed.
- `aria-live` on progress lists and toasts.
- The Trace graph has a **fully equivalent table view**, not a degraded fallback. A graph is unusable with a screen reader, and the table happens to be the better view for a linear trace anyway.
- Tested at 200% zoom and with keyboard only.

---

## 8. Demo mode

`NAGIPATH_DEMO_MODE` renders a permanent banner, disables Probes outright, and disables Credential and User management. Everything else works against seeded fixture data.

This exists so a prospect can click through the real UI — the same binary, the same templates — without us maintaining a separate marketing mock that drifts from the product.

---

## 9. What is deliberately absent

| Absent | Why |
|---|---|
| SPA framework | No build step is the constraint that makes the single-binary distribution work. |
| Client-side data store | Server-rendered means there is no second copy of the truth to go stale. |
| Charts and dashboards beyond simple counts | This is an answering tool, not a monitoring tool. A time-series chart implies data we do not collect. |
| Scores, grades, health percentages | Invites arguing with the number instead of reading the finding. |
| Animations beyond a 150ms panel slide | Nobody in an incident wants to wait for an animation. |
| Onboarding tours and tooltips-as-documentation | If a screen needs a tour, the screen is wrong. |
| Dark patterns around licence limits | Soft enforcement means a banner, and a banner only. |
