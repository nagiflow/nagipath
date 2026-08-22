# Product: Drift Detection

**Pillar 3 of 4.** [PRD-V1 §3.3](../../PRD-V1.md). Backend: [drift.md](../backend/drift.md). UI: [../frontend/drift_report.md](../frontend/drift_report.md).
**Milestone:** M5.

---

## 1. The problem

Forty-one servers are supposed to be identical. They are not, and nobody knows which ones differ or how.

The divergence arrives by ordinary means: an emergency fix applied to one host at 2am and never backported; a host rebuilt from a newer base image; a `location` block added during a migration to three of five servers; a rate limit tuned on the host that was under load. Each was reasonable at the time. Together they mean the fleet's behaviour depends on which server takes the request.

Two related questions get asked, and they are different:

**"Did anything change on this host?"** — asked after an incident, or when something started behaving differently.

**"Does this host match its peers?"** — asked when one server in a pool behaves differently from the rest, which is the hardest class of production problem to diagnose because it is intermittent by construction.

Configuration management tools claim to solve this and, where they are actually applied to the whole fleet, they do. The problem is that in every real enterprise they are applied to *most* of it, and the gap is exactly where the interesting divergence lives.

---

## 2. What nagipath does

Compares parsed configuration objects — not text — against a Baseline, and reports each divergence with the file and line on both sides.

Three Baselines, all computed where applicable:

| Baseline | Answers |
|---|---|
| **Previous Snapshot** | Did anything change here? (temporal) |
| **Golden Peer** | Does this match the designated reference member? (spatial) |
| **Cluster Majority** | Does this match what most members do? (spatial, no designation needed) |

### 2.1 nagipath has no opinion about which side is right

Two words never appear in this feature: *desired state* and *violation*. nagipath does not hold a desired state, does not enforce one, and cannot — v1 writes nothing.

That neutrality is not politeness, it is positioning. A tool that says "violation" is claiming authority over configuration it was never given, and in an enterprise that claim is what gets it uninstalled. A tool that says "these two differ, here are both lines" is useful to everybody and threatening to nobody.

It also happens to be more often correct. When the same divergence appears on thirty-nine of forty-one members, the *Golden Peer* is the outlier — and nagipath says so rather than reporting thirty-nine findings.

### 2.2 Comparing objects, not text

A text diff of two Apache configurations that differ in comment placement and whitespace produces noise. Comparing parsed objects means:

- Findings are grouped by Action Class, so "someone changed access control on three hosts" is one line rather than forty.
- **Reordering is caught.** Two Snapshots with identical Rules in a different order behave differently — order is semantics in every one of these Vendors — so `reordered` is a first-class change type with a generated note explaining why it matters.
- Comparison works across formatting differences that are genuinely cosmetic.

A text diff is still available, because sometimes an operator just wants the bytes.

### 2.3 Phantom drift is structurally impossible

Every comparison re-parses **both** sides at the current parser version.

Without that, upgrading our parser would report differences caused by *our* change rather than the customer's. The operator investigates a change that never happened, finds nothing, and stops opening the report. That single failure mode is why parser version is stored on every derived row and why a stale drift run is recomputed rather than displayed.

---

## 3. Ignore rules decide whether this feature lives

The first Drift report on a real fleet is dominated by differences that are **correct and expected**: bind addresses, per-host `server_name`, log paths containing a hostname, per-environment certificate paths.

Without suppression, the report is noise. The operator closes it. It is never reopened. That is the actual failure mode of every drift tool ever shipped, and it is a product failure, not a technical one.

So:

- **Cluster-scoped rules** with pattern substitutions (`{{node_hostname}}`, `{{node_primary_ip}}`), because a literal-only pattern would need one rule per member.
- **A reason is mandatory.** An ignore with no reason is technical debt that outlives whoever added it.
- **Findings are marked, never deleted.** The row survives with a pointer to the rule that suppressed it.
- **Counts stay visible.** `11 ignored` sits next to `3 findings`, so an ignore list cannot quietly grow until it hides everything.
- **Suggested, never applied.** nagipath proposes candidates from what it observed — "on every member this value equals that member's own hostname" — with a pre-filled reason. The operator confirms.

---

## 4. Journeys

### 4.1 Post-incident: "what changed on web05"

Open the Instance, Previous Snapshot baseline. Three findings: an `add_header` removed, an Upstream Member added, a Route reordered. Each with both lines and a timestamp bounded by the two Collections. Two minutes, and the reordering — which nobody would have spotted in a text diff — is flagged with why it matters.

### 4.2 The intermittent 502

Open the Cluster. 38 of 41 conforming. Two members missing an `add_header`; one on an older version. The two-member finding carries a note: the same divergence on multiple members may mean the Golden Peer is the outlier. That reframing is often the actual answer.

### 4.3 First run on a real fleet

400 findings. Open suggestions: four patterns account for 380 of them, each with an observation and a pre-filled reason. Accept them, and the report reads 20 findings — all real. This journey is the one that determines whether the feature is used again next week.

### 4.4 The patch audit

Version drift is separate from configuration drift, deliberately. "40 Instances on 1.24.0, one on 1.22.1" is a different question with a different owner, and burying it inside a configuration diff hides both.

---

## 5. Acceptance criteria

| # | Criterion |
|---|---|
| 1 | Drift compares parsed objects; a text diff is available as a secondary view. |
| 2 | All three Baselines are computed where applicable, not just the highest-priority one. |
| 3 | Every finding names the change type, the object, and the file and line on both sides. |
| 4 | `reordered` is reported as a change, with a note explaining that order is evaluation order. |
| 5 | Both sides are re-parsed at the current parser version before every comparison. |
| 6 | A drift run behind the current parser version is recomputed, never displayed. |
| 7 | Ignore rules are Cluster-scoped, require a reason, and support host-pattern substitutions. |
| 8 | Ignored findings are marked, retained, and counted visibly beside active findings. |
| 9 | Suggested ignore rules are read-only proposals with an observation and a pre-filled reason. |
| 10 | Cluster view reports conforming count, diverging members, and common divergences that may indicate the Golden Peer is the outlier. |
| 11 | No value with no majority is resolved to a plurality winner; all variants are listed with counts. |
| 12 | A Cluster of one falls back to temporal comparison **and says so**, never reporting clean agreement with itself. |
| 13 | Version drift is reported separately from configuration drift. |
| 14 | A degraded Snapshot on either side is flagged, with a warning that absent objects may be a collection gap rather than a real removal. |
| 15 | Unchanged configuration (matching content hash) skips computation entirely. |
| 16 | The words "violation" and "desired state" appear nowhere in the UI. |

---

## 6. Success signals

**Ignore rules created per install in the first week, and Drift report opens in week four.** The first number shows the noise problem is being handled; the second shows the feature survived it. A tool with heavy week-one usage and zero week-four usage has failed, and this is the pillar where that pattern is most likely.

Supporting: median findings per Instance after suppression (should be small and actionable); Golden Peer designations per Cluster (adoption of the spatial question); count of `reordered` findings, which measures a class of problem no text-diff tool reports at all.

---

## 7. Risks

| Risk | Mitigation |
|---|---|
| First report is unusable noise, feature abandoned | Suggested ignore rules with pre-filled reasons, available on the first report rather than after the operator has already given up. The single most important mitigation in this document. |
| Perceived as a compliance tool the fleet will fail | Explicit neutrality: divergence, not violation. No scores, no grades, no pass/fail. |
| Phantom drift from a parser change | Both-sides re-parse, enforced by design rather than remembered. |
| Ignore list grows until it hides real findings | Counts always visible; mandatory reasons; rules are listed and reviewable on one page. |
| Cluster membership is derived, so a rollout dissolves a cluster mid-flight | Accepted. Membership is identical configuration; a fleet halfway through a rollout genuinely has two configurations, and each half re-forms as its own comparison. Nothing to set up, and no group nobody agreed to. |
