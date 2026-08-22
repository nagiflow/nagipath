# Trace, Hop, External Hop

**Schema:** [schema.md §9](schema.md#9-application-trace-hop). **Conventions:** [api_conventions.md](api_conventions.md).
**Decisions:** ADR-0010 (resolve DNS on the target host), ADR-0004, ADR-0012.
**Product spec:** [../product/request_path_trace.md](../product/request_path_trace.md). **Verification:** [probe.md](probe.md).

---

## 1. Overview

- **Trace** — the ordered chain of Hops a request for an Entry Point takes across the fleet.
- **Hop** — one step in a Trace: a Listener, Site, Route and Upstream on a single Instance.
- **External Hop** — a Hop whose destination is not a managed Instance. A Trace that leaves the fleet ends in one **and says so**, rather than ending silently.

This is the flagship capability. It is also the one where being wrong is worse than being absent, so the whole design is organised around two rules:

1. **Every claim carries its confidence.** Nothing in the trace path can produce `verified`; only Probe evidence can.
2. **Every Trace states why it stopped.** `terminal_reason` is `NOT NULL` with no "unknown" value.

The Trace engine reads only from the database — the current parsed Snapshot per Instance — and never touches the network. That is what makes it fast, reproducible, and testable from checked-in fixtures.

---

## 2. Relationships

`entry_point` → `trace` → `hop` → `hop_rule` → `rule`. A Hop references the `instance`, `snapshot`, `listener`, `site`, `route` and `upstream` it passes through, all `ON DELETE SET NULL` so retention cannot destroy a Trace's shape — only its detail.

`trace.snapshot_set` records exactly which Snapshots the walk used. Without it, a Trace shown to a customer three weeks ago is unreproducible, and "the tool said something different last time" is unanswerable.

> **Open question.** Retention can delete the Snapshots a Trace referenced, leaving it unreproducible. Options are pinning referenced Snapshots or marking such Traces `unreproducible` on screen. Currently assumed: pin for on-screen Traces only, which is a guess. Needs deciding before M3 ships — [schema.md §15](schema.md#15-findings-for-the-decision-record).

---

## 3. The walk

```
Entry Point (scheme, hostname, path, port)
│
├─ 1. Candidate Listeners: every listener across current Snapshots matching port + TLS posture
│
├─ 2. Site selection, per Instance, by hostname:
│       exact server_name / ServerName+ServerAlias / HAProxy hdr acl
│       → wildcard prefix (*.example.com)
│       → wildcard suffix (www.*)
│       → regex
│       → default_server / catch-all
│
├─ 3. Route selection, in Vendor precedence order:
│       ORDER BY precedence_rank, specificity DESC, ordinal
│       with NGINX ^~ short-circuit and regex-first-match semantics
│
├─ 4. Effective Rules for the selected Route, including inherited ones
│       → apply rewrites in order to compute effective_path
│
├─ 5. Upstream → Upstream Members
│
└─ 6. Per Member:
        resolve host via dns_resolution (resolved ON THE NODE)
        ├─ address belongs to a managed Node with a matching Listener → next Hop, go to 2
        └─ otherwise → External Hop, terminal, with a stated reason
```

### 3.1 Why the entry Hop is not obvious

Step 1 is fleet-wide, not "start at the load balancer". nagipath has no concept of a tier, and inferring one would be wrong — in real fleets the same hostname is often served by an HAProxy edge *and* by the NGINX behind it, both configured to answer it. So the engine finds **every** Instance that could receive the request and reports multiple entry candidates when they exist, disambiguated by Listener address and port.

An operator seeing two possible entry points is being told something true and useful. An operator shown one arbitrarily picked entry point is being misled.

### 3.2 Path rewriting between Hops

`hop.effective_path` is the path **after** that Hop's rewrites. Sources:

| Vendor | Mechanism |
|---|---|
| NGINX | `rewrite`, and the `proxy_pass` trailing-slash rule — `proxy_pass http://api/` strips the matched location prefix, `proxy_pass http://api` does not |
| Apache | `RewriteRule` (including inherited ones), `ProxyPass` path mapping, `Alias` |
| HAProxy | `http-request set-path`, `replace-path`, `set-uri` |

This is the field that makes the product's core demo work. The operator sees `/api/v2/charge` arrive at the edge and `/charge` arrive at the application server, with the Rule that did it linked in `hop_rule`. "Where did my path prefix go" is one of the most common and most time-consuming reverse-proxy questions there is.

### 3.3 DNS resolution

Upstream Member hostnames are resolved **on the Node that holds the Upstream**, via `getent hosts`, and cached in `dns_resolution` (ADR-0010).

Split-horizon DNS is normal in segmented enterprises: `app01.corp.example` from the DMZ load balancer and from an internal web server frequently resolve to different addresses, and the control plane's own view is frequently a third answer that matches neither. A future contributor will see a remote command where `net.LookupHost` would do and offer to "fix" it. ADR-0010 exists to stop them.

Two Nodes disagreeing about one name is a **finding to surface**, not an error to reconcile — see `GET /dns-disagreements` in [config_object.md](config_object.md).

### 3.4 Termination

`terminal_reason` values, all of them specific:

| Value | Meaning |
|---|---|
| `external_hop` | Destination resolved but is not a managed Node. Names the address. The most common and most useful terminal state. |
| `no_matching_listener` | No Instance listens on that port with that TLS posture. Response lists the ports that *are* listening on candidate Sites. |
| `no_matching_site` | Listener found, no Site claims the hostname. Response lists nearby hostnames — answers "did I typo it". |
| `no_matching_route` | Site found, no Route matches the path, and no catch-all. |
| `unresolvable_upstream` | `proxy_pass http://$backend`, or a name `getent hosts` cannot resolve on that Node. Honest: the destination genuinely depends on runtime state. |
| `static_content` | The Route serves files (`root`, `alias`, `DocumentRoot`) and does not proxy. A legitimate, complete ending. |
| `hop_limit` | 10 Hops. A fleet with a real 10-hop request path has a problem worth reporting as one. |
| `loop_detected` | The same `(instance_id, route_id)` recurred. Reported as a finding, because it is a genuine misconfiguration. |

There is no `unknown`. A Trace that ends without an explanation is the failure mode this enum exists to prevent.

### 3.5 Branching

A Route may target an Upstream with several Members, and Members may be managed Instances. The result is a **tree**, not a line. It is stored flat as ordered Hops with `is_external` and reconstructed for display; the graph UI renders the branching, and the table view shows the primary path with branches collapsible.

Where a branch is undetermined — an HAProxy ACL the engine cannot evaluate, a `map`-driven `proxy_pass` — the branch is listed verbatim with the reason rather than dropped or assumed false.

---

## 4. Confidence

| State | Meaning | Written by |
|---|---|---|
| `inferred` | Derived from configuration alone. **The default for every Hop and every edge.** | Trace engine |
| `candidate` | A Rule configuration says could apply, with no confirmation it did. Applies to `hop_rule`. | Trace engine |
| `observed_effect` | A Rule whose result is visible in a Probe's response — a header set, a redirect fired, a challenge issued. | Probe |
| `verified` | A Hop confirmed by evidence from the Vendor's own access log, correlated to a specific Probe. | Probe |

`trace.confidence` is the **worst** of its Hops, plus `partial` when they differ. Rounding upward is how a tool becomes untrustworthy in exactly the situation it was bought for.

Every promotion above `inferred` requires a `probe_evidence` row holding the verbatim bytes, so the UI can link straight to the log line. A claim of "verified" the operator cannot audit is worthless.

---

## 5. Caching and invalidation

Traces are computed on demand and cached. A cached Trace is invalidated when any Snapshot in its `snapshot_set` loses `is_current`, or when `parser_version` advances.

A stale Trace is **served with a `stale: true` flag** and recomputed in the background, rather than blocking on a spinner. `computed_at` is displayed on every Trace view — a Trace is a statement about a moment, and presenting one without its timestamp invites someone to act on last week's topology.

### 5.1 Unreproducible Traces

Retention deletes Snapshots on its own schedule and **pins nothing on behalf of a Trace** (ADR-0015). A Trace kept for the historical record can therefore outlive the configuration it walked.

When it does, the Trace becomes **unreproducible**: its shape survives — hop count, ordinals, Instances, `effective_path` per Hop, terminal reason, recorded confidence — while the per-Hop detail that lived in the Snapshot's derived rows is gone. That falls out of the schema rather than needing new code: every derived-row foreign key on `hop` is `ON DELETE SET NULL`, and `hop_rule.rule_id` is `ON DELETE CASCADE`.

The state is **derived, never stored**:

```sql
-- unreproducible when either holds
EXISTS (SELECT 1 FROM hop
         WHERE hop.trace_id = trace.id
           AND hop.is_external = 0
           AND hop.snapshot_id IS NULL)
OR trace.parser_version < :current_parser_version
```

A stored flag would be a second source of truth able to disagree with the rows it describes, and it would need maintaining from inside the retention procedure — the one function that must stay free of conditionals nobody can test.

The API returns `"reproducible": false` with a `reproducible_reason` of `snapshots_pruned` or `parser_version_advanced`, and the affected Hops carry `"detail_available": false`. The correct response is almost always to recompute, so `Retrace` is the primary action offered — which is what makes this degradation recoverable rather than merely honest.

**Live Traces cannot reach this state.** A cached Trace is invalidated the moment any Snapshot in its `snapshot_set` loses `is_current`, and the current Snapshot per Instance is never prunable. Only deliberately retained history degrades.

**Probe evidence is unaffected.** `probe` and `probe_evidence` are exempt from retention, so an unreproducible Trace can still hold the verbatim log lines that verified it. An operator can lose the explanation and keep the proof — evidence is small, cheap and irreplaceable; configuration text is large and recollectable.

---

## 6. API

### `POST /api/v1/traces`

Compute or fetch cached. Either `entry_point_id`, or an ad-hoc URL (the schema enforces exactly one via `CHECK`).

```json
{ "entry_point_id": 11, "refresh": false }
```
```json
{ "url": "https://payments.corp.example/api/v2/charge", "refresh": true }
```

```json
{
  "id": 7712,
  "query": { "scheme": "https", "hostname": "payments.corp.example",
             "path": "/api/v2/charge", "port": 443 },
  "computed_at": "2026-08-21T09:20:11Z",
  "parser_version": 7,
  "snapshot_set": [99121, 99120, 99310],
  "stale": false,
  "confidence": "partial",
  "terminal_reason": "external_hop",
  "hop_count": 4,
  "entry_candidates": [
    { "instance_id": 302, "listener": "0.0.0.0:443", "selected": true,
      "reason": "haproxy frontend fe_https matched hdr_beg(host)" },
    { "instance_id": 301, "listener": "0.0.0.0:443", "selected": false,
      "reason": "also answers this hostname; not selected as entry because lb01 fronts web02 in this trace" }
  ],
  "hops": [
    {
      "ordinal": 0,
      "is_external": false,
      "instance": { "id": 302, "vendor": "haproxy", "display_name": "haproxy (edge)",
                    "node": "lb01.corp.example" },
      "snapshot_id": 99121,
      "listener": { "id": 7100, "address": "0.0.0.0", "port": 443, "tls": true },
      "site": { "id": 8100, "primary_name": "fe_https", "matched_by": "acl hdr_beg(host) payments" },
      "route": { "id": 9100, "match_type": "haproxy_acl_use_backend", "pattern": "path_beg /api",
                 "precedence_rank": 90,
                 "precedence_explanation": "use_backend rules evaluate in file order; first match wins." },
      "upstream": { "id": 6100, "name": "be_payments", "balance_method": "roundrobin" },
      "effective_path": "/api/v2/charge",
      "confidence": "verified",
      "rules": [
        { "rule_id": 55900, "directive": "http-request set-header",
          "args": "X-Forwarded-Proto https", "action_class": "header",
          "confidence": "observed_effect", "inherited_from": null,
          "provenance": { "path": "/etc/haproxy/haproxy.cfg", "line": 88 } }
      ],
      "next": [ { "upstream_member_id": 5100, "host": "web02.corp.example", "port": 443,
                  "resolved_addresses": ["10.20.1.12"], "resolved_on_node_id": 44,
                  "managed_instance_id": 301, "hop_ordinal": 1 } ]
    },
    {
      "ordinal": 1,
      "is_external": false,
      "instance": { "id": 301, "vendor": "nginx", "display_name": "nginx (edge)",
                    "node": "web02.corp.example" },
      "site": { "id": 8001, "primary_name": "payments.corp.example", "matched_by": "exact server_name" },
      "route": { "id": 9001, "match_type": "prefix", "pattern": "/api", "precedence_rank": 40,
                 "precedence_explanation": "Longest matching prefix location. No = or ^~ location matched, and no regex location matched first." },
      "effective_path": "/v2/charge",
      "path_changed_by": [ { "rule_id": 55010, "directive": "rewrite",
                             "args": "^/api/(.*) /$1 break",
                             "from": "/api/v2/charge", "to": "/v2/charge" } ],
      "confidence": "verified",
      "shadowed_rules": [
        { "rule_id": 55004, "directive": "add_header",
          "args": "Strict-Transport-Security max-age=31536000", "scope": "site",
          "reason": "location /api declares its own add_header, discarding all inherited add_header directives" }
      ],
      "next": [ { "upstream_member_id": 5001, "host": "app01.corp.example", "port": 8080,
                  "managed_instance_id": 402, "hop_ordinal": 2 },
                { "upstream_member_id": 5002, "host": "app02.corp.example", "port": 8080,
                  "managed_instance_id": null, "hop_ordinal": 3, "flags": "backup" } ]
    },
    {
      "ordinal": 3,
      "is_external": true,
      "external_target": "10.90.3.11:8080",
      "external_reason": "app02.corp.example resolves to an address that is not a managed Node",
      "confidence": "inferred",
      "arrived_from": { "hop_ordinal": 1, "upstream_member_id": 5002 }
    }
  ],
  "undetermined_branches": [
    { "hop_ordinal": 0, "route_id": 9101,
      "raw_text": "use_backend be_canary if { hdr_sub(cookie) canary=1 }",
      "reason": "ACL expression not evaluable by the trace engine" }
  ],
  "verification": {
    "verified_hop_count": 2,
    "verifiable_hop_count": 3,
    "blockers": [
      { "instance_id": 402, "vendor": "apache",
        "reason": "access log format lacks %v; cannot attribute a request to a specific vhost",
        "suggested_directive": "LogFormat \"%v %h %l %u %t \\\"%r\\\" %>s %b %f\" nagipath" }
    ]
  }
}
```

Six fields here carry the product's honesty commitments, and they are the ones to protect in any refactor:

`entry_candidates` with `selected` and a `reason` — because more than one Instance answering a hostname is normal, and hiding the alternatives is misleading.

`precedence_explanation` — the ordering is the part users disbelieve. Justifying it on screen pre-empts the "why is my exact location losing" support conversation.

`path_changed_by` — the rewrite made visible, with the before and after values. This is the demo.

`shadowed_rules` — NGINX's `add_header` discard rule, which silently removes security headers and is genuinely hard to find by reading configuration.

`undetermined_branches` — what the engine could not evaluate, verbatim, rather than dropped or assumed false.

`verification.blockers` — what would be needed to prove this Trace, with the exact directive. This is what converts "we can't verify" from a shrug into an action.

### `GET /api/v1/traces/{id}`

Cached Trace by ID, unchanged shape. Serves `stale: true` rather than recomputing on read.

### `GET /api/v1/traces?entry_point_id=…&limit=20`

History for one Entry Point, so "when did this path change" is answerable. Returns summaries with `computed_at`, `hop_count`, `confidence`, `terminal_reason` and a `changed_from_previous` flag.

### `GET /api/v1/traces/{id}/graph`

The graph projection for the UI — nodes and edges with confidence on each edge, rather than making the frontend re-derive the tree from a flat Hop array.

```json
{ "nodes": [ { "id": "i302", "label": "lb01 / haproxy", "kind": "instance", "vendor": "haproxy",
               "confidence": "verified" },
             { "id": "x10.90.3.11:8080", "label": "10.90.3.11:8080", "kind": "external",
               "confidence": "inferred" } ],
  "edges": [ { "from": "i302", "to": "i301", "label": "be_payments → web02:443",
               "confidence": "verified", "path_before": "/api/v2/charge",
               "path_after": "/api/v2/charge" } ] }
```

### `GET /api/v1/instances/{id}/traces`

Every cached Trace crossing this Instance — the reverse query behind "what breaks if I take this host out".

---

## 7. Edge cases

| Case | Behaviour |
|---|---|
| Two Instances answer the hostname on the same port | Both returned in `entry_candidates` with reasons. Neither hidden. |
| Hostname matches only a `catch_all` Site | Traced, `matched_by='catch_all'`, with a note that no named Site claimed it. |
| Upstream Member is the Instance itself | `loop_detected`, reported as a finding. Real misconfiguration, not a tool error. |
| Member resolves to an address on a managed Node with no matching Listener | External Hop, reason names the Node and the missing port. More useful than "unknown". |
| Member is a unix socket | Next Hop only if the socket is on the same Node; otherwise External Hop stating a socket is not reachable across hosts. |
| `proxy_pass` to a variable or `map` | `unresolvable_upstream` with `target_raw`. |
| 12-Hop chain | Truncated at 10 with `hop_limit`. The truncation is itself the finding. |
| Degraded Snapshot on the path | Hop flagged, `trace.confidence` drops to `partial`, the degraded Instance named. |
| Unparsed Snapshot on the path | Trace **refuses to continue** through it, terminating with the parse failure named. An empty answer that looks complete is the worst output. |
| Retention deleted a referenced Snapshot | Trace served with `stale` and a note that detail is unavailable; Hop shape survives via `ON DELETE SET NULL`. See the open question in §2. |
| Ad-hoc trace of a hostname with no Application | Fully supported — `entry_point_id` null, `ad_hoc_*` populated. This is the first thing a prospect types in a demo. |
| HTTP Entry Point that redirects to HTTPS | Trace ends with the `redirect` Rule recorded and a `follow_redirect` link to trace the target URL. Silently following it would conflate two distinct request paths into one. |
| Same Trace requested concurrently | Single-flight; both callers get one computation. |
