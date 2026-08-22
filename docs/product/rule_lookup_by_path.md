# Product: Rule Lookup by Context Path

**Sub-capability of pillar 1.** [PRD-V1 §3.1](../../PRD-V1.md). Backend: [rule.md](../backend/rule.md). UI: [../frontend/rule_lookup.md](../frontend/rule_lookup.md).
**Milestone:** M7.

---

## 1. The problem

*"What rules apply to `/api/v2/charge` on `payments.corp.example`?"*

This is a different question from "where does this request go", and it is asked by a different person for a different reason. It is asked when the request arrives somewhere fine but behaves wrong: a header missing, a rate limit firing, a redirect nobody expected, an auth challenge on a path that should be public.

Answering it by hand is worse than tracing, because the answer is not in one place. It requires knowing:

- Which `server` / `<VirtualHost>` / frontend claims that hostname, out of possibly several.
- Which `location` / `<Directory>` / `use_backend` wins for that path — and route precedence in these Vendors is genuinely counter-intuitive.
- Which directives are **inherited** from enclosing scopes and which are overridden.
- Which inherited directives are silently **discarded** — NGINX's `add_header` rule being the notorious case.

`grep` gives all the candidates and none of the resolution. That is the gap.

---

## 2. What nagipath does

Given a hostname and a path, return the Rules **in effect**, ordered, each labelled with its scope, whether it was inherited, whether it is shadowed, and its file and line.

Three parts do the work:

### 2.1 Precedence, computed and justified

Route precedence is computed once at parse time into a `precedence_rank`, per Vendor, and every answer carries a human-readable explanation of why that Route won.

Two subtleties are easy to get backwards and are the ones users challenge:

- **NGINX regex locations are matched in file order, not by length.** A shorter regex earlier in the file beats a longer one later. Prefix locations, by contrast, are matched longest-first. Sorting all locations by length would be wrong for half of them.
- **`^~` is a short-circuit, not a score.** A matching `^~` prefix location stops regex evaluation entirely. Modelling it as "higher priority" produces the right answer for the wrong reason and then produces the wrong answer in the case where a regex would otherwise have won.

Apache's section evaluation order (`<Directory>` shortest-to-longest, then `<DirectoryMatch>`, then `<Files>`, then `<Location>`) and HAProxy's file-order `use_backend`-then-`default_backend` are encoded the same way.

The explanation on screen matters as much as the answer. Users disbelieve route ordering because they have been burned by it, and a bare answer invites "your tool is wrong". An explanation pre-empts the support conversation and teaches the semantics.

### 2.2 Inheritance, resolved

Rules from every enclosing scope are included, each labelled with where it came from and whether an inner scope overrode it. Apache's per-directive inheritance semantics (`RewriteRule` needing `RewriteOptions Inherit`, `Options` merging) are modelled per directive rather than assumed uniform, because they are not uniform.

### 2.3 Shadowing, surfaced

The highest-value output. When `location /api` declares its own `add_header`, **all** `add_header` directives inherited from the enclosing `server` block are discarded. Silently. Security headers vanish and the configuration still looks correct on both lines that produced the problem.

nagipath reports the shadowed Rules explicitly, names the inner directive that caused it, and links both lines. And when a Probe then confirms the header is absent from the live response, it becomes a proven finding rather than an analysis.

---

## 3. Uniform Rules and Action Classes

Every directive is a Rule with the same shape and one of ten Action Classes: match, rewrite, redirect, header, auth, cache, rate_limit, proxy, access_control, other.

This is what makes cross-vendor questions answerable. "Show me every rate limit in the fleet" spans `limit_req`, `mod_ratelimit` and HAProxy `stick-table` without the user needing to know any of those names.

**Directives we have not modelled are still Rules**, with `is_modelled = 0` and their verbatim text. They appear in the answer, marked as unmodelled. That is the honest treatment: the text is never lost, the operator sees it exists, and nagipath does not pretend to interpret it.

The measured frequency of unmodelled directives is our parser backlog, sorted by real customer impact rather than by our guesses about what matters.

---

## 4. Journeys

### 4.1 The missing security header

"Why is HSTS not on `/api`?" Look up the path. `Strict-Transport-Security` appears in the shadowed list with the reason and both lines. Answer in thirty seconds to a question that has consumed afternoons.

### 4.2 The unexpected redirect

"Why does `/health` redirect?" The lookup shows a regex `location` earlier in the file matching first — precedence explanation attached. The operator had been reading the prefix location, which never gets a chance to match.

### 4.3 The auth surprise

"Why is this path asking for a password?" An `auth_basic` inherited from an enclosing scope, named with its origin.

### 4.4 The fleet-wide question

"Every access-control rule across the fleet, grouped by Instance." One query over Action Class. Nobody can do this by hand across three Vendors.

---

## 5. Search

Full-text search over configuration is the companion to structured lookup, with a deliberately unusual tokenizer: separators include `/ . : - _` so that `proxy_pass`, `10.0.0.1`, `/api/v2` and `X-Forwarded-For` are all searchable as the tokens people actually type. A default tokenizer makes exactly the queries an operator wants impossible.

Two indexes: Rules across all retained Snapshots, and raw configuration text for **current** Snapshots only. The current-only limit on text search is stated in every response rather than left to be discovered.

---

## 6. Acceptance criteria

| # | Criterion |
|---|---|
| 1 | Given hostname and path, return Rules in effect ordered by evaluation order. |
| 2 | Every answer includes a human-readable precedence explanation for the winning Route. |
| 3 | NGINX regex locations resolve in file order; prefix locations resolve longest-first. |
| 4 | A matching `^~` location short-circuits regex evaluation. |
| 5 | Apache section evaluation order is honoured, including `<Directory>` shortest-to-longest. |
| 6 | Inherited Rules are labelled with their originating scope. |
| 7 | Shadowed Rules are reported with the inner directive that caused the discard and both file/line references. |
| 8 | Every Rule carries an Action Class; unmodelled directives appear with `is_modelled = 0` and verbatim text. |
| 9 | Cross-vendor queries by Action Class work across all three Vendors. |
| 10 | Anything the engine could not resolve is reported as undetermined with a reason, never omitted. |
| 11 | Every Rule links to file, line and byte range in the stored Snapshot. |
| 12 | Search tokenizes on `/ . : - _` so paths, IPs and header names are searchable. |
| 13 | Text search states its current-Snapshot-only scope in every response. |
| 14 | Unmodelled directive frequency is reportable fleet-wide as a distribution. |
| 15 | Lookup against an Instance with 1,200 Rules returns in under 500 ms. |

---

## 7. Success signals

Lookups per install per week, and the ratio of lookups to Traces — a high ratio means people are using it for behaviour questions and not only topology questions, which is the harder and stickier use case.

The strongest signal: **shadowed-Rule findings acted on.** If operators file tickets off the back of them, the feature has found real problems that were invisible before.

Also tracked: the unmodelled-directive distribution, which is both a product metric and a prioritised work queue.

---

## 8. Risks

| Risk | Mitigation |
|---|---|
| Precedence subtly wrong for one Vendor | Fixture corpus from real configurations with asserted orderings; the two inverted cases documented explicitly in [rule.md §4](../backend/rule.md) so a future contributor does not "simplify" them. |
| A candidate list without resolution is just `grep` | Resolution, precedence explanation and shadowing are the answer. Probe verification closes the remaining gap. |
| Unmodelled directives make answers look incomplete | They appear verbatim and marked. Their frequency is measured and published as the backlog. |
| Apache inheritance is per-directive and easy to over-generalise | Modelled per directive, with the awkward cases (`RewriteOptions Inherit`, `Options` merging) called out in the backend spec. |
| Users challenge the answer | The explanation is the mitigation, and it doubles as documentation of the Vendor's semantics. |
