# Product: Trace Verification

**Cross-cutting.** Backend: [probe.md](../backend/probe.md). UI: [../frontend/trace_explorer.md](../frontend/trace_explorer.md).
**Milestone:** M8.

---

## 1. The problem

A Trace derived from configuration is a **prediction**. It is a very good prediction, but the operator asking the question is often the person who has been burned by a very good prediction before.

The specific failure this feature exists to fix: an earlier design answered "which Rules apply to this path" with a list of eleven candidate Rules. Honest, complete — and **not useful**. Eleven maybes is approximately what `grep` already gives them, and the operator's next question is the one that matters: *which of these actually happened?*

There is also a harder case. The `add_header` shadowing analysis predicts a security header will be silently discarded. That prediction, if right, is a real finding worth escalating. If wrong, it is a false alarm that costs someone an afternoon. Predictions about security posture need proof.

---

## 2. What nagipath does

The operator clicks **Verify** on a Trace. nagipath sends one GET or HEAD request to the Entry Point carrying a unique correlation token, then reads the Vendor's own access log on each Hop and finds that exact request.

Two stages, and the second is the one that changes the product:

**Response evidence** → *Observed Effect.* A header that appeared, a redirect that fired, a challenge that was issued. These prove the Rule's effect is visible.

**Access-log correlation** → *Verified.* The request appears in HAProxy's log naming `fe_https~ be_payments/web02`. That is not inference; that is the load balancer stating what it did, in its own words.

### 2.1 Why we can read their logs

The enabling insight, and the reason this feature is feasible for one engineer: **we already parsed their configuration, so we already know their log format.** The `log_format` / `LogFormat` / `option httplog` directive is a Rule in our database. We parse each Instance's log lines using that Instance's own declared format — named fields, not regex guesses.

No other tool in this space has this, and it comes free from having built a real parser instead of a grep wrapper.

### 2.2 Negative evidence

The sharpest output the product produces.

Configuration says `add_header Strict-Transport-Security` at the Site scope. The shadowing analysis predicts an inner `add_header` discards it. The Probe response arrives — and the header is **absent**.

That is a proven security finding, delivered with the file and line that caused it. Not "you may have a problem". Not a score. A specific header, provably not being served, and the exact directive responsible.

---

## 3. Honesty about limits

This is the feature where it would be easiest to cheat, and where cheating would be most damaging. So the limits are shown in the UI, not buried in documentation.

| Vendor | Out of the box | Why |
|---|---|---|
| **HAProxy** | **Strong** | `option httplog` names frontend, backend and server on every line. Best case in the product. |
| **Apache** | **Good** | `%v` gives the serving vhost; `%f` shows `proxy:http://backend/...`. Stock `combined` lacks `%v` — detectable, one-directive fix. |
| **NGINX** | **Requires a customer change** | Stock `combined` has **neither** `$server_name` nor `$upstream_addr`. A log line proves arrival, not Site selection or destination. |

The NGINX gap is the awkward one, and it is stated plainly in three places: on the Instance, in the Trace's `verification.blockers`, and on the Probe result. **nagipath never works around it by inference.** An honest Inferred beats a confident wrong Verified.

Where a Hop cannot be verified, nagipath shows the exact directive that would enable it — and states that it will not make the change itself. The moment the product edits a customer's log format, it is no longer read-only, and read-only is what gets it installed.

---

## 4. Guardrails

A Probe sends real traffic to a production application. Every guardrail is structural rather than procedural:

- **GET or HEAD only**, enforced by a database `CHECK` constraint. Adding `POST` requires changing a constraint and passing review — exactly the friction that should exist.
- **Never scheduled, never automatic.** No job kind can create one, and none may be added.
- **Always attributed.** `actor_user_id` is `NOT NULL`; there is no unattributed Probe.
- **Self-identifying.** `User-Agent: nagipath-probe/<version> (+correlation:<token>)`. The request announces itself rather than looking like an attack, so a security team reviewing their own logs can identify it immediately.
- **Origin disclosed**, redirect-capped at 5, rate-limited per user and per Entry Point, admin-only, audited, and refused outright in demo mode.

The UI states next to the button that a GET may still have side effects in a badly behaved application. That belongs on the screen, not only in the manual.

### 4.1 Correlation by token, not timestamp

A 128-bit token per Probe, matched in the log by value. Clock skew between the control plane and Nodes is routine; a timestamp-window match would produce false positives under load and false negatives under skew. Token matching has neither failure mode. The Probe records which carrier matched — header, query parameter, or User-Agent.

The query-parameter carrier is **opt-in and defaults off**, because some applications route on query strings and appending one changes the request being tested.

---

## 5. Journeys

### 5.1 Verifying an incident Trace

Trace shows four Hops, all Inferred. Click Verify. Seven seconds later: HAProxy Hop **Verified** with the log line quoted; NGINX Hop **Verified for arrival**, with Site selection still Inferred and the reason stated; Apache Hop **blocked**, with the `LogFormat` directive to add. Confidence becomes `partial`, honestly.

### 5.2 The security finding

Trace flags a shadowed `Strict-Transport-Security`. Verify. The response has no HSTS header. The finding becomes **disproved effect**: configured, not served, with `location /api`'s own `add_header` named at `api.conf:21` as the cause. That is a ticket someone can act on today.

### 5.3 Readiness, before anyone clicks

`GET /entry-points/{id}/verification-readiness` answers "can this be verified" up front — 3 of 4 Hops verifiable, one blocked, here is the directive. Nobody is surprised by a partial result after the fact.

### 5.4 Repeated Probes as a change detector

The cheapest possible change signal for an Entry Point: "this path verified through three Hops last week and now stops at two."

---

## 6. Acceptance criteria

| # | Criterion |
|---|---|
| 1 | A Probe is GET or HEAD only, enforced at the database level. |
| 2 | Every Probe records an actor, an origin host, a URL and a timestamp, and writes an audit event. |
| 3 | No scheduled or automatic path can create a Probe. |
| 4 | Correlation is by token, never by timestamp. |
| 5 | Log lines are parsed using that Instance's own parsed log format. |
| 6 | Every confidence promotion is backed by a `probe_evidence` row holding the verbatim bytes, linked from the UI. |
| 7 | Response headers matching header/redirect/auth Rules promote them to Observed Effect. |
| 8 | A configured header absent from the response is reported as a disproved effect with the causing directive's file and line. |
| 9 | An unverifiable Hop states a specific reason and, where applicable, the exact directive that would enable verification. |
| 10 | nagipath never modifies a log format, and says so where the suggestion appears. |
| 11 | Log reads are restricted to the access-log paths derived at parse time; arbitrary file reads are impossible through this path. |
| 12 | Token matching runs on the Node so only matching lines cross the network. |
| 13 | `viewer` receives `403` and a denied audit event. |
| 14 | Demo mode refuses Probes. |
| 15 | A four-Hop Probe including log correlation completes within 15 seconds. |

---

## 7. Success signals

Probes per Trace (verification demand); percentage of Hops that verify per Vendor (validates the capability table against reality); **number of customers who change an NGINX `log_format` because nagipath asked them to** — that one measures whether the honest-limit approach actually persuades rather than merely disclaims; and count of disproved-effect security findings, the highest-value output.

---

## 8. Risks

| Risk | Mitigation |
|---|---|
| A customer treats a Probe as unauthorised traffic against production | Self-identifying User-Agent, origin host recorded, admin-only, per-Probe initiation, full audit. Documented for their change-control process. |
| NGINX verification requires customer action, so most Hops stay partial | Stated up front via readiness; partial arrival verification still delivered; the exact fix shown. |
| Logs shipped off-host with nothing local | Detected and reported as a blocker. SIEM integration is out of v1 scope and not pretended. |
| Pressure to infer verification where logs are silent | The one line to hold: an honest Inferred beats a confident wrong Verified. Written into the ADR so it survives a future contributor. |
| A GET with side effects | Cannot be prevented technically. GET/HEAD-only, per-Probe initiation, and a warning on the button itself. |
