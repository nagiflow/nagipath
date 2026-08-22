# Product: Request Path Trace

**Pillar 1 of 4.** [PRD-V1 §3.1](../../PRD-V1.md). Backend: [trace.md](../backend/trace.md). UI: [../frontend/trace_explorer.md](../frontend/trace_explorer.md).
**Milestone:** M3 — the first demoable release.

---

## 1. The problem

An operator is handed a URL and asked where it goes.

Today that answer takes between twenty minutes and two days, and it is arrived at by: SSH to the load balancer, `grep` the config, find a backend name, `grep` the backend definition, resolve the hostname (from the wrong host, getting the wrong answer), SSH to that host, discover it also runs NGINX, `grep` again, find a `proxy_pass`, notice the trailing slash, wonder whether the path prefix survives, SSH to the next host, find Apache with fourteen included files, run `httpd -S`, and finally reach something that serves content.

Every step of that is mechanical. Every step is also error-prone in a way that is invisible: the wrong `location` block looks right, the trailing slash looks like formatting, and the DNS answer from your laptop looks authoritative.

Three groups feel this differently:

**The operator who inherited the fleet.** Nobody who built it is still here. There is no diagram, or there is a diagram from 2019 that is confidently wrong. Every question requires re-deriving the topology from scratch.

**The engineer in an incident.** 3am, a 502, and the question is which of four hops broke. Every minute spent deriving the path is a minute not spent fixing it, and the derivation is done under exactly the conditions where mistakes get made.

**The developer who does not have SSH.** They know their hostname. They cannot see anything else, so they file a ticket and wait — and the operator answering it does the same twenty minutes of grepping, for the fourth time this month.

---

## 2. What nagipath does

Type a URL. Get the chain of Hops it takes across the fleet, each one naming the Instance, the Site, the Route that matched, the Rules in effect, the Upstream, and the path as it arrives at the next Hop.

Each claim is labelled with how confident nagipath is, and every Trace says why it stopped.

### 2.1 The three things that make it worth using

The chain alone is table stakes. Three specific outputs are why an operator would choose this over grep:

**The path transformation, made visible.** `/api/v2/charge` arrives at the edge; `/v2/charge` arrives at the application server; the `rewrite` that did it is linked with its file and line. "Where did my path prefix go" is one of the most common and most time-expensive reverse-proxy questions there is, and the answer usually turns out to be a trailing slash on a `proxy_pass` that nobody noticed.

**Precedence, justified on screen.** When two Routes could match, nagipath states which won and *why* — "longest matching prefix; no `=` or `^~` location matched, and no regex location matched first". Users disbelieve route ordering, correctly, because they have been burned by it. Justifying it pre-empts the whole "your tool is wrong" conversation and teaches the semantics at the same time.

**Shadowed Rules.** NGINX's `add_header` inheritance rule — an inner `add_header` discards *all* inherited ones — silently removes security headers. It is genuinely hard to find by reading configuration and trivial for a parser to find. When a Probe then confirms the header is absent from the live response, the finding goes from "we think" to proven, with the line that caused it.

### 2.2 What it deliberately does not do

- It does not guess. An unevaluable HAProxy ACL is reported verbatim as an undetermined branch, not resolved by assumption.
- It does not pick one entry point when several are possible. Two Instances answering the same hostname is normal, and hiding one is misleading.
- It does not follow HTTP→HTTPS redirects silently. It reports the redirect and offers to trace the target as a separate Trace, because they are two different request paths.
- It does not claim `verified` from configuration alone. Only Probe evidence promotes confidence.

---

## 3. Journeys

### 3.1 Incident: "the payments API is 502ing"

1. Paste `https://payments.corp.example/api/v2/charge` into the trace box.
2. Four Hops render in under two seconds: HAProxy on lb01 → NGINX on web02 → Apache on app01 → an External Hop at `10.90.4.7:8443`.
3. The External Hop is the answer. `app02.corp.example` resolves to an address that is not a managed Node — so it is either unmanaged or newly built, and either way it is where to look next.
4. Run a Probe. Three of four Hops verify from access logs; the fourth is blocked because that Apache's `LogFormat` lacks `%v`, and nagipath shows the exact directive that would fix it.

Time: under two minutes, from a URL to a specific host to investigate.

### 3.2 Inheritance: "which servers serve this hostname"

An operator new to a fleet types the hostname with no path. The Trace shows two entry candidates — an HAProxy edge and an NGINX that also answers it directly. That second one is news, and it is the kind of news that explains months of intermittent inconsistency.

### 3.3 Pre-change: "what breaks if I take web05 out"

Open the Instance, read the reverse query: every Application whose Traces cross it, with Hop ordinals. This is the question a hand-maintained CMDB answers confidently and wrongly.

### 3.4 Developer self-service (v1.1 in full, partially v1)

A developer logs in as a `viewer`, types their own hostname, and sees their path without filing a ticket. In v1 they see the whole fleet, which is acceptable for a first release and is the reason Application-scoped viewers are the first v1.1 item.

---

## 4. Acceptance criteria

| # | Criterion |
|---|---|
| 1 | Given a URL, return the ordered chain of Hops across the fleet, or a specific `terminal_reason` explaining why it could not. |
| 2 | Every Hop names Instance, Node, Site, matched Route, effective Rules, and Upstream. |
| 3 | Every Route selection carries a human-readable `precedence_explanation`. |
| 4 | `effective_path` reflects rewrites, and `path_changed_by` names the Rule with before and after values. |
| 5 | Every claim carries a confidence label. Nothing in the trace path emits `verified`. |
| 6 | `terminal_reason` is always populated and never `unknown`. |
| 7 | Traces that leave the fleet end in an External Hop naming the address and the reason. |
| 8 | Branching Traces render as a tree with backup and weighted Members distinguished. |
| 9 | Unevaluable branches are listed verbatim with a reason, never dropped. |
| 10 | Every derived claim links to a file path and line number in the stored Snapshot. |
| 11 | Upstream hostnames are resolved on the Node holding the Upstream, and the resolving Node is named. |
| 12 | A cached Trace displays `computed_at`; a stale one is served flagged rather than blocking. |
| 13 | A cross-vendor Trace (HAProxy → NGINX → Apache) works end to end. |
| 14 | Ad-hoc URLs work with no Application configured — the first thing a prospect types. |
| 15 | Trace of a 4-Hop path over a 40-Instance fleet returns in under 2 seconds warm, under 5 cold. |

---

## 5. Success signals

The one that matters: **at least one design partner's Trace reveals a hop its owner did not know existed.** That is the moment the product stops being a nicer `grep` and becomes something they cannot reproduce by hand. It is also, deliberately, a signal we cannot fake — either the fleet contains a surprise or it does not.

Supporting signals: Traces run per week per install; ratio of ad-hoc URL Traces to Entry Point Traces (high ad-hoc means people reach for it during incidents, which is the goal); how often a Trace is followed by a Probe (verification demand); how often `undetermined_branches` is non-empty (our parser backlog, measured rather than guessed).

---

## 6. Risks

| Risk | Mitigation |
|---|---|
| A wrong Trace destroys trust faster than no Trace builds it | Confidence labelling on every claim; refusal to guess; `undetermined_branches` shown verbatim. The design's central commitment. |
| Precedence implemented subtly wrong per Vendor | Fixture corpus from real configurations, with expected orderings asserted; `precedence_rank` computed once at parse time rather than branched in queries. |
| Fleets where most Traces immediately hit an External Hop | Still useful — "your fleet boundary is here" is an answer. But it caps the value, so design-partner selection should favour fleets with real multi-hop depth. |
| Operators expect it to trace application-internal routing | Scope stated plainly: nagipath traces to the last managed web server, and says so. |
