# Application and Entry Point

**Schema:** [schema.md §9](schema.md#9-application-trace-hop). **Conventions:** [api_conventions.md](api_conventions.md).
**Decisions:** ADR-0008 (an Application is a set of Entry Points).

---

## 1. Overview

- **Application** — a named set of Entry Points with an owner.
- **Entry Point** — a hostname plus optional path prefix at which an Application is reached. The starting point of a Trace.

An Application's Instances, Sites, Upstreams and Certificates are **derived by tracing from its Entry Points, never enumerated by hand**.

There is no `application_instance` membership table, and adding one would defeat the design (ADR-0008). This is the single most likely thing for a future contributor to "fix", so it is stated in the ADR, in the schema, and here.

---

## 2. Why derivation rather than membership

Every CMDB in every enterprise contains an application-to-server mapping, and every one of them is wrong. It is wrong because it is maintained by hand, updated at deployment time by someone who remembers, and never updated at decommission time by anyone.

The insight the product is built on: **an operator already knows the hostname their application is reached at, and that hostname is enough.** Everything else — which of forty NGINX Instances serves it, which HAProxy frontend fronts it, which Apache backend terminates it, which Certificates it depends on — is discoverable from configuration. So it should be discovered, every time it is asked, rather than recorded once and left to rot.

The consequences are the point:

- An Application's footprint is always current, because it is computed from the latest Snapshots.
- Adding a backend server to an Upstream automatically brings it into the Application's footprint. Nobody has to remember.
- Removing it removes it. Nobody has to remember that either.
- "Which Applications does `app07` serve?" is the reverse query, answered by finding Traces that cross it — which is the question asked before decommissioning a host, and the one a hand-maintained mapping answers wrongly and confidently.

The cost is real and worth stating: an Application's footprint is only as good as the Trace. Where a Trace ends in an External Hop, the Application's footprint ends there too, and the UI must say so rather than implying completeness.

---

## 3. Relationships

| | |
|---|---|
| `application` → `entry_point` | One-to-many, cascade. An Application with no Entry Points is legal but inert and flagged in the UI. |
| `entry_point` → `trace` | One-to-many. Traces are computed and cached, not authored. |
| `entry_point` → `probe` | One-to-many. |
| `application` → Instances / Sites / Upstreams / Certificates | **No foreign keys.** Derived through Traces. |

`application.owner` is free text in v1. It becomes a foreign key to a user or LDAP group in v1.1, when developers log in and Application-scoped viewer permissions arrive — which is exactly the scope column the one-chokepoint `authz.Can()` design was built to accept.

---

## 4. Business logic

### 4.1 Entry Point validation

- `hostname` — a DNS name or IP literal. No scheme, no path, no port. Wildcards rejected: an Entry Point is a place a request actually arrives, not a pattern.
- `path_prefix` — must start with `/`, defaults to `/`. Normalised (collapse `//`, strip trailing `/` except root).
- `scheme` — `http` or `https`. `port` null means the scheme default.
- Unique per Application on `(scheme, hostname, path_prefix)`.
- The same hostname may belong to **different Applications at different path prefixes** — `corp.example/payments` and `corp.example/hr` are two Applications. This is the normal enterprise shape and the reason `path_prefix` is part of the identity rather than an optional filter.

### 4.2 Overlap detection

Two Entry Points in **different** Applications where one path prefix contains the other is not an error — it is usually a shared edge hostname — but it is worth surfacing, because the more specific one wins at request time and the owner of the broader one often does not know it.

`GET /applications/{id}/overlaps` reports it. nagipath takes no position on which is correct.

### 4.3 Footprint computation

On demand, cached, invalidated when any Snapshot in a contributing `trace.snapshot_set` is superseded:

```
for each Entry Point:
    trace it (see trace.md)
    collect Instances, Sites, Routes, Upstreams, Upstream Members from every Hop
    collect Certificate Bindings on every Listener the Trace traversed
union across Entry Points, de-duplicate, retain the worst confidence encountered
```

**Worst confidence wins.** An Application whose Traces are three-quarters Verified and one-quarter Inferred is reported as partial, never as Verified. Rounding confidence upward is how a tool becomes untrustworthy in exactly the situation it was bought for.

### 4.4 What an Application is not

Not an RBAC scope in v1 (two roles, global). Not a deployment unit. Not an environment — `payments-prod` and `payments-uat` are two Applications, because they have different Entry Points and different Instances, and pretending otherwise would require the environment dimension the PRD's original six-level hierarchy assumed and that v1 deliberately does not have.

---

## 5. API

### `GET /api/v1/applications`

```json
{
  "items": [
    {
      "id": 4,
      "name": "payments-prod",
      "owner": "payments-platform@corp.example",
      "entry_point_count": 2,
      "footprint": {
        "instance_count": 6,
        "vendors": ["haproxy", "nginx", "apache"],
        "node_count": 5,
        "upstream_member_count": 8,
        "external_hop_count": 1,
        "certificate_count": 3,
        "soonest_certificate_expiry": "2026-09-14T00:00:00Z",
        "open_drift_findings": 2,
        "confidence": "partial",
        "computed_at": "2026-08-21T09:20:11Z",
        "stale": false
      }
    }
  ]
}
```

The footprint block is the Application list's whole reason to exist: an owner opens it to see *how many moving parts my application depends on and which of them is about to break*, and both of those come from tracing rather than from anything anyone typed.

### `POST /api/v1/applications`

```json
{ "name": "payments-prod", "owner": "payments-platform@corp.example",
  "description": "",
  "entry_points": [ { "scheme": "https", "hostname": "payments.corp.example", "path_prefix": "/" },
                    { "scheme": "https", "hostname": "api.corp.example", "path_prefix": "/payments" } ] }
```

Creates both in one transaction and returns the Application with a first footprint already computed, because a newly created Application showing zeros looks broken.

### `PATCH /api/v1/applications/{id}` · `DELETE`

`name`, `owner`, `description`. Delete cascades Entry Points and their Traces; **never** touches Instances, Snapshots or Probes. Deleting the label cannot delete the fleet data — an Application is a view, and views are cheap to discard.

### `POST /api/v1/applications/{id}/entry-points` · `DELETE /api/v1/entry-points/{id}`

`422` on invalid hostname, wildcard, or non-normalisable path. `409` on duplicate within the Application.

### `GET /api/v1/applications/{id}/footprint`

Full expansion, not counts:

```json
{
  "application_id": 4,
  "confidence": "partial",
  "computed_at": "2026-08-21T09:20:11Z",
  "entry_points": [
    { "id": 11, "hostname": "payments.corp.example", "path_prefix": "/",
      "trace_id": 7712, "hop_count": 3, "confidence": "verified",
      "terminal_reason": "external_hop" }
  ],
  "instances": [
    { "id": 302, "vendor": "haproxy", "node": "lb01.corp.example", "role": "entry",
      "hop_ordinals": [0] },
    { "id": 301, "vendor": "nginx", "node": "web02.corp.example", "role": "intermediate",
      "hop_ordinals": [1] },
    { "id": 402, "vendor": "apache", "node": "app01.corp.example", "role": "terminal",
      "hop_ordinals": [2] }
  ],
  "external_hops": [
    { "trace_id": 7712, "hop_ordinal": 3, "target": "10.90.4.7:8443",
      "reason": "not a managed Node — footprint beyond this point is unknown" }
  ],
  "certificates": [
    { "id": 88, "subject_cn": "payments.corp.example", "not_after": "2026-09-14T00:00:00Z",
      "binding_count": 2, "days_remaining": 24 }
  ],
  "coverage_note": "1 of 4 Hops leaves the managed fleet. This footprint is complete only up to that point."
}
```

`coverage_note` and the `external_hops` array are non-negotiable. An Application footprint that silently stops at the fleet boundary reads as complete and is the most dangerous possible output — the operator concludes there is nothing further to check.

### `GET /api/v1/applications/{id}/overlaps`

```json
{ "items": [ { "our_entry_point": { "hostname": "api.corp.example", "path_prefix": "/payments" },
               "other_application": { "id": 9, "name": "api-gateway" },
               "other_entry_point": { "hostname": "api.corp.example", "path_prefix": "/" },
               "relationship": "our path is more specific and wins at request time" } ] }
```

### `GET /api/v1/instances/{id}/applications`

The reverse query. Every Application whose Traces cross this Instance, with the Hop ordinal and confidence. This is the pre-decommission check, and it is the query a hand-maintained CMDB answers confidently and wrongly.

### `POST /api/v1/applications/{id}/retrace`

Recomputes every Trace and the footprint. `202`.

---

## 6. Edge cases

| Case | Behaviour |
|---|---|
| Application with no Entry Points | Legal, footprint empty, flagged as inert in the UI. Not an error — it is a half-finished onboarding step. |
| Entry Point hostname matches no Site anywhere | Trace returns `terminal_reason='no_matching_site'` with the hostnames that *were* found nearby. Answers "did I typo it, or is it genuinely not configured here". |
| Two Applications claim the identical Entry Point | Both allowed, both flagged. Usually a real ownership dispute the tool should expose, not resolve. |
| Same hostname, different path prefixes, different owners | The normal enterprise shape. Fully supported; `path_prefix` is part of Entry Point identity. |
| Entry Point on a port no Listener uses | `terminal_reason='no_matching_listener'`, listing the ports that *are* listening on candidate Sites. |
| Trace crosses a degraded Snapshot | Footprint confidence drops to `partial` and the degraded Instance is named. |
| Every contributing Snapshot unparsed | Footprint returns empty with `coverage_note` explaining the parse failure. Never a silent zero. |
| Underlying Snapshot superseded | Cached footprint marked `stale: true` and recomputed on next read. Stale data is served with the flag rather than a spinner. |
| Application deleted while a Probe is running | Probe completes; its evidence rows survive. `probe.entry_point_id` goes null via `ON DELETE SET NULL`; audit history is intact. |
| Owner leaves the company | Free text, so it silently rots. Accepted v1 limitation; the v1.1 LDAP work replaces it with a group reference. |
