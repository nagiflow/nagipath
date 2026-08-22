# Rule Lookup

**Assumes:** [design_system.md](design_system.md). **Backend:** [rule.md](../backend/rule.md).
**Product:** [../product/rule_lookup_by_path.md](../product/rule_lookup_by_path.md). **Milestone:** M7.

---

## 1. What this screen is

The sibling of the Trace Explorer, for a different question.

Trace answers *where does this request go*. Rule Lookup answers *what happens to it on the way* — which directives are in effect for a hostname and path, in what order, inherited from where, and which of them are silently not applied.

It is the screen someone opens when the request arrives fine but behaves wrong: a header missing, an auth challenge on a public path, a redirect nobody configured, a rate limit firing.

Routes: `/rules/lookup`, deep-linkable as `/rules/lookup?host=payments.corp.example&path=/api/v2/charge`.

---

## 2. Layout

```
┌────────────────────────────────────────────────────────────────────────────┐
│  Rule lookup                                                               │
│  host  [ payments.corp.example        ]  path  [ /api/v2/charge      ]      │
│  instance  [ all instances serving this host ▾ ]            [ Look up ]     │
├────────────────────────────────────────────────────────────────────────────┤
│  2 instances serve this host · 14 rules in effect · 1 shadowed · 2 unmodel. │
├────────────────────────────────────────────────────────────────────────────┤
│  web02.corp.example · nginx 1.24.0                                         │
│  ─────────────────────────────────────────────────────────────────────────  │
│  site   payments.corp.example         exact server_name    api.conf:3       │
│  route  prefix /api                                        api.conf:14      │
│         longest matching prefix; no = or ^~ location matched, and no        │
│         regex location matched first                                        │
│                                                                            │
│  ⚠ 1 rule configured for this path is not in effect               show ▾    │
│                                                                            │
│  Rules in effect                        group by: [ class ▾ ] order ⇅       │
│  ┌──────────────────────────────────────────────────────────────────────┐   │
│  │ proxy                                                             3  │   │
│  │   proxy_pass         http://payments_api/     location  api.conf:15 │   │
│  │   proxy_set_header   Host $host                server   api.conf:8  │   │
│  │                                                inherited            │   │
│  │   proxy_read_timeout 60s                       http      nginx.conf:22│  │
│  │                                                inherited            │   │
│  ├──────────────────────────────────────────────────────────────────────┤   │
│  │ header                                                            2  │   │
│  │   add_header  X-Frame-Options SAMEORIGIN      location  api.conf:21 │   │
│  │   add_header  X-Content-Type-Options nosniff  location  api.conf:22 │   │
│  ├──────────────────────────────────────────────────────────────────────┤   │
│  │ rate_limit                                                        1  │   │
│  │   limit_req  zone=api burst=20 nodelay        location  api.conf:19 │   │
│  ├──────────────────────────────────────────────────────────────────────┤   │
│  │ other · not interpreted by nagipath                               2  │   │
│  │   some_third_party_directive foo bar          location  api.conf:31 │   │
│  │     nagipath stores this directive but does not interpret it        │   │
│  └──────────────────────────────────────────────────────────────────────┘   │
│                                                                            │
│  web05.corp.example · nginx 1.24.0                            differs ⚠    │
│  ─────────────────────────────────────────────────────────────────────────  │
│  … 13 rules in effect · 1 rule differs from web02      [ compare ]          │
└────────────────────────────────────────────────────────────────────────────┘
```

---

## 3. Input behaviour

**Host** — Autocompletes from `site_name` across the fleet, so the operator does not need to remember exact spelling. Free text is allowed, since the point is often to discover that a hostname is not served anywhere.

**Path** — Free text, must start with `/`. Client-side normalisation shows what will actually be looked up when the input differs (`//api` → `/api`), because a silent normalisation makes a surprising result unexplainable.

**Instance** — Defaults to **all Instances serving this host**, not a single one. That default is the design decision worth defending: an operator asking "what rules apply" usually assumes one answer, and the truth is frequently that two servers answer the same hostname with different rules. Forcing a single-Instance choice hides the discrepancy; showing all of them surfaces it as the answer.

When Instances differ, the second and subsequent ones carry a `differs ⚠` chip and a `compare` action.

**Submit** — Also on `Enter` from either field. Results replace the panel via htmx with `hx-push-url`.

---

## 4. The resolution header

Site and Route, each with how they were selected and a provenance link. The precedence explanation is inline and in full sentences, not abbreviated.

This is where users challenge the tool, so the explanation is the answer's equal partner. Examples per Vendor:

| Vendor | Explanation |
|---|---|
| NGINX prefix | `longest matching prefix; no = or ^~ location matched, and no regex location matched first` |
| NGINX regex | `first matching regex location in file order; regex locations are evaluated in the order they appear, not by length` |
| NGINX `^~` | `^~ prefix location matched, which stops regex evaluation entirely` |
| Apache | `<Directory> sections are evaluated shortest path first; this is the longest matching directory, so its directives are applied last and win` |
| HAProxy | `use_backend rules are evaluated in file order; this is the first match` |

The NGINX regex and `^~` cases exist as separate explanations because they are the two people get backwards, and a generic "highest priority match" would be technically defensible and pedagogically useless.

---

## 5. Shadowed rules

Collapsed, warning-coloured, expanded on click. The highest-value block on the screen.

```
⚠ 1 rule configured for this path is not in effect                     hide ▴
  ───────────────────────────────────────────────────────────────────────────
  add_header Strict-Transport-Security "max-age=31536000" always
    configured at   server payments.corp.example        api.conf:9
    discarded by    location /api declaring its own add_header
                                                        api.conf:21
    why             in nginx, an add_header in an inner block discards ALL
                    add_header directives inherited from enclosing blocks.
                    Re-declare it inside location /api to keep it.
    [ Verify with a probe ]     [ Show both lines side by side ]
```

Four things are present on purpose:

- **Both provenance links.** The configured line and the discarding line — the operator needs both to fix it.
- **The `why`.** This rule is genuinely surprising, and the operator's first reaction is that we are wrong.
- **The remedy.** One sentence, stated as fact, not as an offer to apply it.
- **`Verify with a probe`.** Turns analysis into proof. When the Probe confirms the header is absent, the block turns danger-coloured and reads **Disproved — configured but not served**, and it becomes a ticket someone files today.

---

## 6. Rules in effect

Grouped by Action Class by default; toggle to strict evaluation order. Both views matter — class grouping answers "what does this path do", evaluation order answers "why did this one win".

Each row: directive, arguments, scope chip, `inherited` marker with its origin, confidence badge (`candidate` until a Probe promotes it), provenance link.

Filters as chips: Action Class, scope, `inherited only`, `unmodelled only`.

**Unmodelled Rules are a group, not a footnote.** Verbatim text, an `unmodelled` chip, and the line *nagipath stores this directive but does not interpret it*. Two reasons: the operator needs to know the directive is there, and they need to know we are not claiming to have understood it. A tool that quietly omits what it does not parse is a tool whose completeness cannot be trusted anywhere.

---

## 7. Cross-instance comparison

`compare` opens a side-by-side of the two Instances' effective Rules, aligned by directive, with differences highlighted.

This is frequently the actual answer. "It works on web02 and not on web05" is one of the most common shapes of production problem, and the difference is usually one directive that was added to one host during an incident and never backported.

---

## 8. Fleet-wide distribution

`/rules/distribution` — the companion view, reached from the lookup screen's footer.

- Rule counts by Action Class across the fleet.
- Every Instance's rate limits, or access controls, or auth rules, in one table. Cross-vendor, because Action Class abstracts over `limit_req` / `mod_ratelimit` / `stick-table`.
- **Top unmodelled directives by frequency**, with the Instances they appear on.

That last table is our parser backlog, measured against real customer configurations rather than guessed. It is shown to the customer as well, because it is an honest statement of coverage and it invites exactly the feedback we want.

---

## 9. Loading, error and empty states

| State | Treatment |
|---|---|
| Initial, no query | `Enter a hostname and path.` Below: three hostnames from the fleet as one-click examples. |
| Loading | Skeleton rows per Instance. Under 500ms typically, so no staged messaging. |
| Path missing leading `/` | Inline: `Paths must start with /`. Not auto-corrected — silently changing the query makes the result unexplainable. |
| Host serves nowhere | `No site in this fleet claims payments.corp.exmaple.` Then nearest hostnames found. Answers "did I typo it" without requiring the user to suspect a typo. |
| Host served, path matches no Route and no catch-all | Site named, `no route matches /foo`, then the Routes that do exist in evaluation order. |
| Route matched, zero Rules | `This route has no directives of its own and inherits none.` A real answer, styled neutrally. Never an empty table. |
| Multiple Instances, identical results | Rendered once with `identical on 4 instances` and an expander listing them. Four identical panels is noise. |
| Multiple Instances, differing results | Each shown, differences chipped, `compare` offered. |
| Instance's Snapshot degraded | Amber chip: `collected with gaps — some directives may be missing`. Critical distinction: a missing Rule may be a collection gap, not an absent directive. |
| Instance's Snapshot unparsed | That Instance shows the parse error with a link to browse the text; other Instances still render. One bad parse does not blank the screen. |
| `viewer` clicks `Verify with a probe` | Disabled with the role hint on hover. |
| Demo mode | Verify disabled with `probes are disabled in demo mode`. |
| More than 200 Rules in effect | Paginated within the class group with `Load more`. No truncation without saying so. |

---

## 10. Acceptance criteria

| # | Criterion |
|---|---|
| 1 | Host and path produce the Rules in effect, or a specific reason none could be resolved. |
| 2 | Default scope is every Instance serving the host, not one. |
| 3 | Differing results across Instances are flagged and comparable side by side. |
| 4 | Identical results across Instances are collapsed to one panel with a count. |
| 5 | Site and Route selection each show a full-sentence precedence explanation inline. |
| 6 | NGINX regex-in-file-order and `^~` short-circuit have their own distinct explanations. |
| 7 | Shadowed Rules show both provenance links, the Vendor semantics, and the remedy. |
| 8 | `Verify with a probe` promotes a shadowed Rule to a proven finding when confirmed. |
| 9 | Rules can be grouped by Action Class or shown in strict evaluation order. |
| 10 | Inherited Rules name their originating scope. |
| 11 | Unmodelled Rules appear as a group with verbatim text and an explicit non-interpretation note. |
| 12 | Every Rule links to file, line and byte range. |
| 13 | Paths are not silently normalised; a difference is shown before lookup. |
| 14 | A host that is served nowhere returns nearest-hostname suggestions. |
| 15 | A degraded Snapshot warns that missing directives may be a collection gap. |
| 16 | One Instance's parse failure does not prevent others from rendering. |
| 17 | Fleet-wide distribution reports top unmodelled directives by frequency. |
| 18 | Every lookup is deep-linkable. |
| 19 | Lookup against an Instance with 1,200 Rules renders in under 500 ms. |
