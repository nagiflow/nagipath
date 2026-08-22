# Product: Collection Scheduling and Retention

**Supporting capability.** Backend: [collection.md](../backend/collection.md), [node.md](../backend/node.md). UI: [../frontend/settings.md](../frontend/settings.md).
**Milestone:** M0, refined through M5.

---

## 1. The problem

Every feature in nagipath is downstream of a Collection having succeeded recently. So two unglamorous questions decide whether the product works in practice:

**How does data stay fresh without anyone thinking about it?** A tool that needs manual refreshing shows stale data at the moment it is needed, because the moment it is needed is 3am during an incident and nobody refreshed it.

**How does a year of history not fill the disk?** Forty Instances × 60 files × daily collection is 876,000 file captures a year. Stored naively that is unusable on a small VM, and "you need more disk" is a bad answer for a tool whose pitch is that it installs in an afternoon.

There is a third question that only shows up in production: **how does one broken host not poison everything?** A Node that is decommissioned but still in the inventory will fail every collection forever, filling logs, wasting workers, and — worst — generating alert fatigue that makes real failures invisible.

---

## 2. What nagipath does

An in-process scheduler. No cron, no external queue, no separate worker process — the binary schedules its own work, which is what keeps the deployment story to one file.

- **Per-Node schedule**, default daily with a global default and per-Node override.
- **Jitter** on every scheduled run, so 200 Nodes do not all collect at the same second.
- **Bounded concurrency** — 8 SSH workers globally, 1 per Node. A Node is never collected twice simultaneously.
- **Quarantine after 10 consecutive failures.** Scheduled collection stops; manual collection still works and clears the quarantine on success.
- **Content-addressed storage.** Unchanged configuration costs one hash comparison and zero bytes.
- **Retention by count and age**, with a floor: the current Snapshot is never pruned.

### 2.1 Quarantine is the alert-fatigue fix

Ten consecutive failures is not a transient problem. It is a decommissioned host, a rotated credential nobody told us about, or a firewall change. Retrying it forever produces a permanently red dashboard, and a permanently red dashboard is one nobody looks at.

Quarantine says: *this is not a transient failure, it needs a human, and until then it will stop consuming workers and attention.* The Node stays visible with its failure reason and last successful collection. Manual collection always works, so the fix-and-verify loop is one click.

### 2.2 Storage economics, concretely

Content-addressed zstd blobs keyed by SHA-256. Forty Instances sharing a `mime.types` file store one copy. A year of daily Collections on a fleet that changes rarely stores approximately the same bytes as one Collection, plus one row per file per Snapshot.

This is what makes daily collection with a year of retention a reasonable default on a 2 GB VM, and it is the reason the history features (Drift baselines, "show me this file in March") can exist at all rather than being cut for space.

Retention deletes `snapshot_file` rows first, then garbage-collects blobs that no row references. Ordering matters: reversed, it deletes shared content still in use.

### 2.3 Freshness is displayed, always

Every view showing derived data shows when its Snapshot was collected. A Trace without a timestamp invites someone to act on last week's topology, and during an incident that is actively harmful.

Where an Instance is quarantined or its last Collection is degraded, that state travels with the data into every downstream view rather than being visible only on the Node page.

---

## 3. Degraded collection is a first-class outcome

A Collection that captures configuration but cannot read a certificate, or captures three of four Instances, is **degraded** — not failed and not successful.

Recording it as success hides a gap that then reads as a real absence: an object missing because we could not see it looks identical to an object that was removed. That confusion is exactly what makes a drift report untrustworthy, so degradation is stored on the Snapshot, named specifically ("`openssl` not found on the target"), and surfaced wherever the data is used.

---

## 4. Journeys

### 4.1 Setup, once

Global default: daily at 02:00 with jitter, retain 30 Snapshots or 90 days, whichever is larger. Set once during onboarding. Nobody touches it again.

### 4.2 The decommissioned host

`web12` was decommissioned in March; nobody removed it from the inventory. Ten failures later it is quarantined with "connection refused" and its last successful Collection date. The operator retires the Node. The dashboard goes green — and stays a meaningful green.

### 4.3 The rotated credential

A credential rotates and eight Nodes start failing. The failure list groups by reason, making the common cause obvious immediately. Update the Credential, bulk-collect, quarantines clear.

### 4.4 The pre-change collection

Before a planned change, the operator collects the affected Cluster on demand so the Drift baseline is exactly the pre-change state. Manual collection is always available, including on quarantined Nodes.

### 4.5 The disk-space conversation

Settings shows database size, blob count, deduplication ratio and what a given retention change would reclaim — before it is applied. Nobody should have to guess at the cost of a retention setting.

---

## 5. Acceptance criteria

| # | Criterion |
|---|---|
| 1 | Collection is scheduled in-process; no external scheduler or worker process is required. |
| 2 | Global default schedule with per-Node override. |
| 3 | Scheduled runs are jittered. |
| 4 | Global SSH concurrency is bounded and configurable; per-Node concurrency is 1. |
| 5 | 10 consecutive failures quarantines a Node from scheduled collection. |
| 6 | Manual collection always works, including on a quarantined Node, and success clears quarantine. |
| 7 | Every Node shows last attempt, last success, consecutive failure count and last failure reason. |
| 8 | Failures are grouped by reason so a common cause is visible at a glance. |
| 9 | Identical file content is stored once fleet-wide. |
| 10 | An unchanged configuration short-circuits on content hash without re-storing. |
| 11 | Retention is by count and age; the current Snapshot is never pruned. |
| 12 | Blob GC runs only after dependent rows are deleted. |
| 13 | Retention preview shows reclaimable space before the change is applied. |
| 14 | Degraded Collections are recorded as degraded with a specific reason, never as success. |
| 15 | Degraded and stale state travels into every downstream view. |
| 16 | Every view of derived data displays its Snapshot's collection time. |
| 17 | A 40-Node bulk Collection completes within 10 minutes at 8 workers. |
| 18 | Collection failure on one Node never blocks or fails another. |

---

## 6. Success signals

**Collection success rate above 95% steady-state, with quarantined Nodes trending to zero after week two.** The second half matters more: quarantines that persist mean the operator has stopped caring about the dashboard.

Supporting: deduplication ratio (validates the storage design against real fleets); percentage of Collections that are degraded, broken down by cause, which tells us exactly which environment assumption to fix next; how often manual collection is used, which indicates whether the schedule is trusted.

---

## 7. Risks

| Risk | Mitigation |
|---|---|
| In-process scheduling caps horizontal scale | Accepted and bounded. `job.leased_until` exists unused in the schema so multi-process leasing is an addition rather than a rewrite. |
| Server restart during collection | Jobs are database rows; in-flight work is reclaimed on start-up. Nothing depends on process memory surviving. |
| Collection load on production hosts | Read-only commands, bounded concurrency, jitter, and a documented resource profile. `nginx -T` on a large config is the heaviest single operation and it is measured. |
| Retention deletes something a Trace needed | Traces use `ON DELETE SET NULL` so shape survives and detail degrades visibly. The open question about pinning referenced Snapshots is recorded in [schema.md §15](../backend/schema.md#15-findings-for-the-decision-record) and must be settled before M3 ships. |
| Database growth on a large fleet | Deduplication, zstd, retention defaults, and a visible size figure with a preview before any change. |
