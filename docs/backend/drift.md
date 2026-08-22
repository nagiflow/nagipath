# Drift and Baseline

**Schema:** [schema.md §11](schema.md#11-drift). **Conventions:** [api_conventions.md](api_conventions.md).
**Decisions:** ADR-0012 (parser version and re-parse policy), ADR-0007 (natural-key identity), ADR-0002 (read-only).
**Product spec:** [../product/drift_detection.md](../product/drift_detection.md).

---

## 1. Overview

- **Drift** — a divergence between an Instance's current configuration and its Baseline.
- **Baseline** — what Drift is measured against: either the Instance's previous Snapshot, or its Cluster's Golden Peer, or the Cluster majority.

Two words are deliberately absent from this entire document, and from the UI: *desired state* and *violation*. nagipath has **no opinion about which side of a divergence is correct**. It does not hold a desired state, does not enforce one, and cannot — v1 writes nothing (ADR-0002). Drift is a divergence reported neutrally; the operator decides what it means.

That neutrality is not politeness. A tool that says "violation" is claiming authority over configuration it was never given, and in an enterprise that claim is what gets it uninstalled.

---

## 2. Relationships

`instance` → `drift_run` → `drift_finding`, with `drift_ignore_rule` scoped to a `cluster`. A `drift_run` references both the subject Snapshot and the baseline Snapshot; both cascade on Snapshot deletion, so retention removes stale runs cleanly.

---

## 3. Baseline selection

Per Instance, in order:

1. **`golden_peer`** — the Instance's Cluster has a designated `golden_peer_instance_id`, and this Instance is not it.
2. **`cluster_majority`** — the Instance is in a Cluster of ≥3 members with no Golden Peer.
3. **`previous_snapshot`** — everything else: no Cluster, a Cluster of one or two, or the Golden Peer itself.

All three are computed and stored when applicable, not just the highest-priority one. The two questions are genuinely different and operators ask both:

- *"Did anything change here?"* → `previous_snapshot`. Temporal.
- *"Does this match its peers?"* → `golden_peer` / `cluster_majority`. Spatial.

A Cluster of one falls back to `previous_snapshot`, and the UI **says so** rather than reporting "no drift" from a majority of one. Reporting agreement with oneself as a clean result is a lie of omission.

### 3.1 Cluster majority

Per natural key, the value held by more than half the members. Where no value has a majority — three members with three different values — the finding is reported as `no_majority`, listing all variants with their member counts. Picking a plurality winner would manufacture a Baseline nobody chose.

---

## 4. Comparison

Drift compares **parsed objects**, not text.

```
subject  = derived rows of the subject Snapshot,  re-parsed at current parser_version
baseline = derived rows of the baseline Snapshot, re-parsed at current parser_version

for each object kind:
    key both sides by (natural_key, ordinal)
    in subject not baseline          → added
    in baseline not subject          → removed
    in both, attributes differ       → changed  (per differing field)
    in both, same text, ordinal moved → reordered
apply ignore rules → mark, never delete
```

### 4.1 Both sides are always re-parsed

This is the load-bearing rule of the whole feature. A `drift_run` stores `parser_version`, and any comparison re-parses **both** Snapshots at the *current* version before comparing.

Without it, a diff spanning a parser upgrade reports differences caused by **our parser changing** rather than by the customer's configuration changing. That is **phantom Drift**, and it is uniquely corrosive: the operator investigates a change that never happened, finds nothing, and stops trusting the report. A `drift_run` whose `parser_version` is behind current is stale and recomputed, never displayed (ADR-0012).

### 4.2 `reordered` is a real change

Two Snapshots with identical Rule sets in different order behave differently — order is semantics in every one of these Vendors. A diff that reported "no change" would be wrong, so `change` has a dedicated `reordered` value.

### 4.3 Text comparison is available but secondary

`GET /snapshots/{id}/diff` also returns a unified text diff per file, because sometimes an operator just wants to see the bytes. Structured findings are the primary output because they are filterable, groupable by Action Class, and comparable across Vendors; a text diff of two Apache configurations that differ in whitespace is noise.

---

## 5. Ignore rules

The feature that decides whether Drift is used or abandoned.

The first Drift report on a real fleet is dominated by differences that are **correct and expected**: bind addresses, hostnames, `server_name` per host, instance IDs, log paths containing a hostname, certificate paths per environment. Without a way to suppress them, the report is noise, the operator closes it, and it is never reopened. That is the actual failure mode of every drift tool, and it is a product failure rather than a technical one.

```sql
drift_ignore_rule (cluster_id, object_kind, field, pattern, reason, created_by, created_at)
```

| Property | Rationale |
|---|---|
| **Cluster-scoped** | Expected host-specific differences are a property of how that Cluster is built, not a global truth. |
| **`reason` is `NOT NULL`** | An ignore with no reason is technical debt that outlives whoever added it. |
| **Findings are marked, never deleted** | `ignored_by_rule_id` is set; the row survives. |
| **Counts stay visible** | `drift_run.ignored_count` is shown next to `finding_count`, so an ignore list cannot quietly grow until it hides everything. |
| **Suggested, never auto-applied** | nagipath proposes candidates from observed patterns; the operator confirms each with a reason. |

Suggestion heuristic: a difference whose value on every member equals that member's own hostname, primary IP, or Node display name is a strong candidate, and the suggestion pre-fills the reason with what it observed.

---

## 6. Version drift is separate

A change in `instance.version` or `build_flags` is recorded as a distinct event, **not** as a configuration finding. "Someone patched NGINX on 40 of 41 Instances" and "someone changed a `location` block" are different questions with different owners, and conflating them buries both.

`GET /clusters/{id}/version-drift` answers the first directly.

---

## 7. API

### `GET /api/v1/drift`

Fleet-wide. Filters: `instance_id`, `cluster_id`, `baseline_kind`, `object_kind`, `action_class`, `change`, `include_ignored`, `since`.

```json
{
  "items": [
    {
      "drift_run_id": 5510,
      "instance": { "id": 305, "display_name": "nginx (edge)", "node": "web05.corp.example",
                    "vendor": "nginx" },
      "cluster": { "id": 2, "name": "web-prod" },
      "baseline_kind": "golden_peer",
      "baseline_label": "web02 / nginx (edge)",
      "computed_at": "2026-08-21T09:15:40Z",
      "parser_version": 7,
      "finding_count": 3,
      "ignored_count": 11,
      "findings": [
        {
          "id": 91001,
          "object_kind": "rule",
          "natural_key": "route:9005|add_header|3",
          "change": "removed",
          "action_class": "header",
          "field": "",
          "baseline_text": "add_header Strict-Transport-Security \"max-age=31536000\" always;",
          "subject_text": "",
          "provenance_baseline": { "path": "/etc/nginx/conf.d/api.conf", "line": 27 },
          "provenance_subject": null,
          "ignored_by_rule_id": null
        },
        {
          "id": 91002,
          "object_kind": "upstream_member",
          "natural_key": "payments_api|app03.corp.example:8080",
          "change": "added",
          "action_class": "",
          "subject_text": "server app03.corp.example:8080 weight=1;",
          "provenance_subject": { "path": "/etc/nginx/conf.d/api.conf", "line": 9 },
          "ignored_by_rule_id": null
        },
        {
          "id": 91003,
          "object_kind": "route",
          "natural_key": "prefix:/api",
          "change": "reordered",
          "field": "ordinal",
          "baseline_text": "ordinal 2",
          "subject_text": "ordinal 5",
          "note": "Route order is evaluation order. This changes which Route wins for overlapping paths.",
          "ignored_by_rule_id": null
        }
      ]
    }
  ]
}
```

The `note` on `reordered` findings is generated, and it exists because "the same lines in a different order" reads as cosmetic to anyone who has not internalised that these Vendors evaluate in order.

### `GET /api/v1/instances/{id}/drift?baseline_kind=…`

All three Baseline comparisons for one Instance, so temporal and spatial questions are answerable side by side.

### `GET /api/v1/clusters/{id}/drift`

Cluster-wide matrix — the highest-value screen in this pillar.

```json
{
  "cluster": { "id": 2, "name": "web-prod", "member_count": 41 },
  "baseline_kind": "golden_peer",
  "baseline_label": "web02 / nginx (edge)",
  "conforming_count": 38,
  "diverging": [
    { "instance_id": 305, "node": "web05.corp.example", "finding_count": 3,
      "worst_action_class": "header" },
    { "instance_id": 319, "node": "web19.corp.example", "finding_count": 1,
      "worst_action_class": "access_control" }
  ],
  "common_divergences": [
    { "natural_key": "route:9005|add_header|3", "change": "removed",
      "affected_instance_count": 2,
      "note": "The same divergence on 2 members may indicate the Golden Peer is the outlier." }
  ],
  "no_majority_fields": [],
  "version_drift": { "versions": { "1.24.0": 40, "1.22.1": 1 },
                     "outliers": [ { "instance_id": 319, "version": "1.22.1" } ] }
}
```

`common_divergences` inverts the question. When the same divergence appears on many members, the Golden Peer is more likely to be the odd one out — and telling the operator that is more useful than reporting thirty-nine identical findings.

### `GET /api/v1/snapshots/{id}/diff?against={id}&format=structured|text`

Both sides re-parsed at the current parser version before comparison, always.

### `GET /api/v1/drift/ignore-rules` · `POST` · `DELETE`

```json
{ "cluster_id": 2, "object_kind": "listener", "field": "address",
  "pattern": "{{node_primary_ip}}",
  "reason": "each member binds its own primary IP; expected by design" }
```

`422 reason_required` when `reason` is blank. `{{node_primary_ip}}`, `{{node_hostname}}` and `{{node_display_name}}` are the supported substitutions — a literal-only pattern would need one ignore rule per member, which nobody would maintain.

### `GET /api/v1/drift/ignore-rules/suggestions?cluster_id=2`

```json
{ "items": [ { "object_kind": "site_name", "field": "name",
               "suggested_pattern": "{{node_hostname}}",
               "would_suppress": 41,
               "observed": "on every member this value equals that member's own hostname",
               "prefilled_reason": "per-host server_name; expected" } ] }
```

Read-only. Applies nothing.

### `POST /api/v1/instances/{id}/recompute-drift`

`202`. Also enqueued automatically when a parser upgrade advances `parser_version`.

---

## 8. Edge cases

| Case | Behaviour |
|---|---|
| Configuration unchanged since last Collection | `content_sha256` matches; Drift computation skipped entirely. |
| First Snapshot for an Instance | No `previous_snapshot` Baseline. Golden Peer comparison still runs if it is in a Cluster; otherwise "no baseline yet", never "no drift". |
| Cluster of one | Falls back to `previous_snapshot` with an explicit note. Never reports clean agreement with itself. |
| Three members, three different values | `no_majority`, all variants listed with counts. No plurality winner invented. |
| Golden Peer retired | Cluster falls back to majority; the UI flags that the designation is dangling. |
| Golden Peer is the outlier | `common_divergences` surfaces it with a note. nagipath does not reassign it. |
| Mixed-Vendor Cluster | Prevented at membership (`422`). Comparing NGINX to Apache produces a diff where every line differs. |
| Upstream renamed | One `removed`, one `added` (ADR-0007). Accepted over fuzzy rename detection, which produces confident wrong answers. |
| Parser upgraded between Collections | Both sides re-parsed at current version. **Phantom Drift is structurally impossible**, not merely unlikely. |
| Baseline Snapshot pruned by retention | `drift_run` cascades away. Recomputed against the nearest surviving Baseline, with the substitution stated. |
| Degraded subject or baseline Snapshot | Findings still reported, flagged degraded, and the UI warns that absent objects may be a collection gap rather than a real removal. This distinction matters enormously and must not be glossed. |
| Unparsed Snapshot on either side | No comparison. Reported as "cannot compare, parse failed" with the error. Never an empty clean result. |
| Ignore rule suppresses everything | Allowed, and `ignored_count` stays visible beside `finding_count` so it is obvious. |
| 41 members × 1,200 Rules | Comparison is set-based on indexed `(natural_key, ordinal)`, computed once per Collection, not per page view. |
