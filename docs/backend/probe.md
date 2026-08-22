# Probe and Verification

**Schema:** [schema.md §10](schema.md#10-probe-and-verification). **Conventions:** [api_conventions.md](api_conventions.md).
**Decisions:** ADR-0002 (read-only; Probe is the single documented exception), ADR-0004.
**Product spec:** [../product/trace_verification.md](../product/trace_verification.md).

---

## 1. Overview

A **Probe** is a single operator-initiated GET or HEAD request against an Entry Point, sent to verify a Trace. Never scheduled, never automatic, always audited.

Probes exist because of a specific, concrete product failure. An earlier design answered "which Rules apply to this path" with a list of eleven candidates. That is honest, and it is also **not useful to an admin** — it is roughly what `grep` already gives them. The list is the starting point; proving which Hops actually served the request is what turns it into an answer.

Verification works in two stages, and the second is the one that matters:

1. **Response evidence** — headers, status, redirect chain → **Observed Effect** on the Rules whose result is visible.
2. **Access-log correlation** — read the log on each candidate Instance, find *this* Probe's request → **Verified** on those Hops.

The enabling insight for stage 2: **we know how to read their access logs because we already parsed their configuration.** The `log_format` / `LogFormat` / `option httplog` directive is a Rule in our database, so we can parse a log line into named fields for that specific Instance rather than guessing at a format.

---

## 2. Probe is the one exception to read-only

v1 writes nothing to any managed host (ADR-0002). A Probe sends network traffic to a production application, which is a different thing from writing to a host, but it is still the one action nagipath takes that has any effect outside its own process. The guardrails are therefore structural, not procedural:

| Guardrail | Enforcement |
|---|---|
| GET or HEAD only | `CHECK (method IN ('GET','HEAD'))` **at the database level**. A future request for `POST` must change a constraint and pass review — exactly the friction that should exist. |
| Operator-initiated per Probe | `actor_user_id NOT NULL`. There is no unattributed Probe. |
| Never scheduled | No `job.kind` value can create one, and none may be added. |
| Redirect-capped | `probe_max_redirects`, default 5. |
| Audited | An `audit_event` per Probe with actor, URL and target. |
| Origin disclosed | `probe.origin_host` records where the request came from, so a security team reviewing their own logs can identify it. |
| Identified | A `User-Agent` of `nagipath-probe/<version> (+correlation:<token>)`. The request announces itself rather than looking like an attack. |
| Rate-limited | Per user and per Entry Point: `probe.RateLimited` (§6), `probe_rate_limit_max` per `probe_rate_limit_window_seconds`, both Settings. |
| Admin-only | `viewer` cannot Probe: `s.admin(s.startTraceProbe)`. |
| Disabled in demo | `NAGIPATH_DEMO_MODE` refuses Probes outright: `Server.DemoMode`, checked first in `startTraceProbe` (§6). |

---

## 3. Correlation token

A 128-bit random token, unique per Probe, sent three ways:

- Header: `X-Nagipath-Probe: <token>`
- Query parameter: `?__nagipath=<token>` (appended only when the operator opts in — it changes the request, and some applications route on query strings)
- Embedded in the `User-Agent`

Matching in the access log is **by token, not by timestamp**. Clock skew between the control plane and the Nodes is routine, and a timestamp-window match would produce both false positives under load and false negatives under skew. Token matching has neither failure mode.

Which of the three carriers survives to the log depends on the Vendor's log format — `%r`/`$request` captures the query string, `%{User-Agent}i`/`$http_user_agent` captures the agent, and a custom format may capture the header. The Probe records which carrier matched, in `probe_evidence.parsed_fields`.

---

## 4. Per-Vendor verification capability

This table is the honest limit of the feature and must be visible in the UI, not buried in documentation.

| Vendor | Default capability | What the log gives | Gap |
|---|---|---|---|
| **HAProxy** | **Strong out of the box** | `option httplog` logs `frontend_name/bind_name` and `backend_name/server_name` on every request | None. The log names both the Site and the Upstream Member directly. This is the best case in the product. |
| **Apache** | **Good with common formats** | `%v` (the serving vhost) and `%f`; `%f` shows `proxy:http://backend/...` for proxied requests | Stock `combined` lacks `%v`. Detectable, and the fix is one `LogFormat` directive. |
| **NGINX** | **Requires a customer change** | Stock `combined` has **neither** `$server_name` nor `$upstream_addr` | Without them, a log line proves a request arrived at that Instance but not which Site served it or where it went. nagipath detects this and shows the exact directive to add. It does not make the change. |

The NGINX gap is stated plainly everywhere it matters — on the Instance (`verification_capability`), in the Trace (`verification.blockers`), and on the Probe result. **nagipath never works around it by inference.** An honest Inferred beats a confident wrong Verified, and this is the case where the temptation to guess is strongest.

Even at stock `combined`, an NGINX log line still yields partial evidence: it proves the request reached that Instance at that moment. That grants `verified` to the Hop's *arrival*, and the response says exactly which parts remain `inferred`.

---

## 5. Execution

```mermaid
sequenceDiagram
    participant U as Operator
    participant P as probe
    participant T as trace engine
    participant EP as Entry Point
    participant E as Executor
    participant N as Candidate Instances

    U->>P: POST /probes (explicit, audited)
    P->>T: ensure a fresh Trace exists
    T-->>P: hops + candidate instances + log paths
    P->>P: mint correlation token, insert probe row (result=running)
    P->>EP: GET/HEAD with token, redirect-capped
    EP-->>P: status, headers, redirect chain
    P->>P: response evidence → observed_effect on matching Rules
    Note over P: brief settle delay — logs are buffered
    loop per candidate Instance, in parallel
        E->>N: tail -n <lookback> <access_log_path>, grep token
        N-->>E: matching lines
        P->>P: parse line with THAT Instance's parsed log_format
        P->>P: log evidence → verified on the Hop
    end
    P->>P: unverified hops stay inferred, with the reason stated
    P->>P: result=completed
```

### 5.1 Response evidence → Observed Effect

| Evidence | Promotes |
|---|---|
| Response header matching an `add_header` / `Header set` / `http-request set-header` Rule | That Rule → `observed_effect` |
| 3xx with a `Location` matching a `return`/`Redirect`/`redirect` Rule | That Rule → `observed_effect` |
| `401` with `WWW-Authenticate`, or a redirect to an identity provider | The `auth`-class Rule → `observed_effect` |
| A header a Rule should have set but that is **absent** | Recorded as **negative evidence** — the strongest signal the shadowing analysis produces |

Negative evidence is the sharpest thing in this document. If the configuration says `add_header Strict-Transport-Security` at the Site scope, the shadowing analysis predicted it would be discarded by an inner `add_header`, and the Probe confirms the header is absent — that is a **proven** security finding, delivered with the file and line that caused it. That is the single most compelling output the product can produce, and it falls out of design rather than being built for.

### 5.2 Log evidence → Verified

Reading the log uses `Executor.ReadFile` / a `tail` command restricted to the Instance's `access_log_paths`, which were derived at parse time. Path validators reject anything outside those declared paths, so log correlation cannot become an arbitrary file read.

`probe_log_lookback_seconds` (default 120) bounds how much log is read. A short settle delay before reading accounts for buffered writes (`access_log ... buffer=`, `BufferedLogs On`).

Each matching line is parsed with that Instance's own log format into `parsed_fields`, and the verbatim line is stored in `raw_evidence`. Every promotion above `inferred` requires such a row — a "verified" claim the operator cannot audit is worthless, so the UI links straight to the log line.

---

## 6. Interface

There is no `/api/v1/probes` JSON API in v1. Probe is one of the server-rendered
screens like every other page in the product — a classic form-POST-then-redirect,
not a REST surface. The real, live route:

### `POST /trace/probe`

Admin only — `s.admin(s.startTraceProbe)` in `internal/web/web.go`, the same
middleware every mutating route uses; a viewer gets `403`. Form-encoded, not JSON:

```
url=https://payments.corp.example/api/v2/charge&method=GET&trace=7712
```

`trace` is the id of an already-saved Trace. When it is `0` — a Probe launched
straight from a pasted URL rather than from an existing Trace page — the handler
saves one first: a Probe's evidence attaches to hop rows, and there has to be a
Trace for it to attach to.

On success: `303` to `/trace?...&run=<id>`, and the Trace page polls that run
(`GET /trace?...&run=<id>`, served from the in-memory registry in
`internal/web/probelive.go`) until it completes. On refusal — demo mode, the rate
limit, an unreachable target, a URL nagipath will not send — `303` to
`/trace?...&err=<message>`, the same `fail()`/redirect pattern every other admin
action in this product uses for reporting a problem back to the operator. Both
guardrails below run inside this handler, before `probe.Prober.Run` is ever called,
so neither can let a request reach past them to the fleet:

- **Demo mode.** Refused outright when `$NAGIPATH_DEMO_MODE` is set to any
  non-empty value at server startup (`Server.DemoMode`, read once in
  `cmd/nagipath/main.go`). `err=this is a demo instance; NAGIPATH_DEMO_MODE refuses
  every probe`.
- **Rate limit.** `probe.RateLimited` counts this operator's Probes at this Entry
  Point — matched by the exact `url`, the same key `probe.Recent` and `probe.Last`
  already use for history, so no `entry_point_id` lookup is needed — within the last
  `probe_rate_limit_window_seconds` (Settings, default `300`) and refuses once
  `probe_rate_limit_max` (Settings, default `5`) is reached. `err=rate limit
  reached: N probe(s) already sent to this entry point in the last Ws by this
  operator`. Setting `probe_rate_limit_max` to `0` disables the guardrail rather
  than blocking everything.

Both refusals still write an `audit_event` with `outcome='denied'` — a Probe
nagipath declined to send is still something an operator asked it to do, and the
"Audited" guardrail in §2 covers the refusal, not just the request.

### Reading a Probe back

There is no `GET /api/v1/probes/{id}`. A completed Probe is read back through two
functions in `internal/probe`, both queried by `url` and rendered inline on the
Trace page rather than returned as JSON:

- `Last(ctx, db, url)` — the most recent Probe at that URL (`probe.Past`: the
  response fields, plus its `probe_evidence` rows as `EvidenceRow`, each carrying
  the Hop it applies to, `grants` (`observed_effect` / `verified` / `disproved`),
  and the verbatim `raw_evidence` a Verified claim has to be auditable against).
- `Recent(ctx, db, url, limit)` — history for that Entry Point, newest first
  (`probe.Record`): repeated Probes over time are the cheapest possible change
  detector — "this path was verified through three Hops last week and now stops at
  two."

`disproved` is an evidence `grants` outcome, not a stored `hop_rule.confidence`
value — the Rule exists and is configured; what is disproved is its *effect*. That
distinction keeps the confidence enum honest: it describes evidence about
behaviour, not the presence of configuration.

There is no `verification-readiness` endpoint either; the Instance's
`verification_capability` and the Trace's own blockers (§4) already answer "can
this be verified" wherever they are shown, without a Probe having to run first.

---

## 7. Edge cases

| Case | Behaviour |
|---|---|
| Entry Point not reachable from the nagipath host | `result='failed'` with the transport error. Reported as *our* connectivity problem, not as a fleet finding — the operator's fleet may be fine. |
| Probe returns 500 | Verification still works. A failing application is still an excellent verification signal, and the Trace is what was asked about. |
| Token absent from every log | Every Hop stays `inferred`, each with a specific reason: format lacks the carrier, log path unreadable, buffered write not yet flushed, or request genuinely did not reach that Instance. Never a bare "not verified". |
| Log rotated between request and read | Reads the current file and the most recent rotated one. If still absent, reason names rotation. |
| Log is piped to a program (`CustomLog "\|/usr/bin/cronolog …"`) | Not readable. Detected at parse time and reported as a blocker, with the pipe target named. |
| Log shipped off-host with nothing local | Same blocker path. Reading from a SIEM is not in v1 scope and is not pretended. |
| High-traffic Instance, thousands of lines in the lookback | Token grep is done on the Node (`grep` before transfer), so only matching lines cross the network. |
| Two Probes concurrently on one Entry Point | Distinct tokens, no interference. |
| Application routes on query strings | `include_query_token` defaults to **false** for exactly this reason; the operator opts in knowing it changes the request. |
| Redirect loop | Capped at 5, `result='completed'` with the chain recorded. The loop is the finding. |
| mTLS-required Entry Point | Probe fails at TLS handshake with a specific reason. Client certificates are not in v1 scope. |
| Probed path has side effects despite GET | Cannot be prevented technically. Mitigated by GET/HEAD-only, per-Probe operator initiation, and documentation. Stated in the UI next to the button rather than only in the manual. |
| Viewer attempts a Probe | `403` plus an `audit_event` with `outcome='denied'`. |
