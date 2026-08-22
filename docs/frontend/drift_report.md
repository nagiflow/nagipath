# Drift Report

**Assumes:** [design_system.md](design_system.md). **Backend:** [drift.md](../backend/drift.md).
**Product:** [../product/drift_detection.md](../product/drift_detection.md). **Milestone:** M5.

---

## 1. What this screen is

Where configuration divergence is reported — neutrally.

The word "violation" appears nowhere. Neither does "desired state", "compliance", or a score. nagipath has no opinion about which side of a divergence is correct, and the UI's job is to make that stance obvious in its language, not just in its documentation. A tool that says "violation" is claiming authority over configuration it was never given.

The screen has one real design problem, and it dominates everything else: **the first report on a real fleet contains 400 findings, and 380 of them are correct and expected.** If the operator cannot get to the 20 that matter in the first two minutes, they close the tab and never come back. That is the actual failure mode of every drift tool ever shipped.

So the suppression workflow is not a settings page. It is the first thing on this screen.

Routes: `/drift`, `/clusters/{id}/drift`, `/instances/{id}/drift`, `/drift/ignore-rules`.

---

## 2. `/drift` — Fleet-wide

```
┌────────────────────────────────────────────────────────────────────────────┐
│  Drift                                                  computed 2h ago    │
│                                                                            │
│  ┌──────────────────────────────────────────────────────────────────────┐   │
│  │ ⓘ 380 of 400 differences look like expected per-host variation.       │   │
│  │   4 suggested rules would suppress them.        [ Review suggestions ]│   │
│  └──────────────────────────────────────────────────────────────────────┘   │
│                                                                            │
│  baseline [ golden peer ▾ ]  cluster [ all ▾ ]  class [ all ▾ ]            │
│  change [ all ▾ ]   ☐ show ignored (11)                                    │
│                                                                            │
│  20 findings · 11 ignored                                                  │
│                                                                            │
│  web-prod · golden peer web02 / nginx (edge)          38 of 41 conforming   │
│  ─────────────────────────────────────────────────────────────────────────  │
│  ⚠ removed   header          add_header Strict-Transport-Security …         │
│              web05, web19    baseline api.conf:27 · subject —      [ open ] │
│              ⓘ this difference appears on 2 members — the golden peer       │
│                may be the outlier                                          │
│                                                                            │
│    added     proxy           server app03.corp.example:8080 weight=1        │
│              web05           subject api.conf:9                    [ open ] │
│                                                                            │
│    reordered match           location /api  ordinal 2 → 5                   │
│              web05                                                 [ open ] │
│              ⓘ route order is evaluation order. This changes which route    │
│                wins for overlapping paths.                                  │
│                                                                            │
│  app-prod · previous snapshot                                              │
│  ─────────────────────────────────────────────────────────────────────────  │
│    changed   cache           ExpiresDefault  "access plus 1 hour"           │
│              app01                        → "access plus 1 day"    [ open ] │
└────────────────────────────────────────────────────────────────────────────┘
```

Grouped by Cluster, each group naming its Baseline in plain language. The conforming count is shown per group so the report is not read as "the fleet is broken".

### 2.1 The suggestions banner

Top of the page, before the findings, on the very first report. It is the most important element on the screen.

It exists because the sequence *see 400 findings → understand that most are noise → find the settings page → author four patterns → return* has too many steps, and the operator gives up at step two. Putting the suppression path in front of the noise is the difference between a feature that survives week one and one that does not.

The banner disappears once no suggestion would suppress anything.

### 2.2 Finding rows

Each row: change type, Action Class, the object, affected Instances, both provenance links, `open`.

Design choices that matter:

- **`reordered` gets a generated note** explaining that order is evaluation order. Without it, "the same lines in a different order" reads as cosmetic to anyone who has not internalised that these Vendors evaluate in order.
- **The Golden-Peer-outlier note** appears when the same divergence affects multiple members. Reporting thirty-nine identical findings is less useful than telling the operator their reference host is the odd one out.
- **Multiple Instances collapse into one row** with the instance list. Forty rows for one divergence is the noise problem in a different costume.
- **The ignored count is always visible** next to the active count, and `show ignored` is one checkbox away. An ignore list must never be able to grow quietly until it hides everything.
- **Baseline is named in words** — `golden peer web02 / nginx (edge)`, `previous snapshot 2026-08-20 02:14` — because "drift" without knowing what against is meaningless.

Clicking a row opens the detail panel: both sides in full, side-by-side diff, both provenance links, and `[ Ignore this pattern ]`.

---

## 3. `/clusters/{id}/drift` — Cluster matrix

The most valuable view in this pillar.

```
┌────────────────────────────────────────────────────────────────────────────┐
│  web-prod · 41 members · golden peer web02 / nginx (edge)     [ change ▾ ]  │
│                                                                            │
│  38 conforming    3 diverging                                              │
│                                                                            │
│  Diverging members                                                         │
│    web05    3 findings   worst: header                            [ open ] │
│    web19    1 finding    worst: access control                    [ open ] │
│    web31    1 finding    worst: cache                             [ open ] │
│                                                                            │
│  Differences shared by several members                                     │
│    add_header Strict-Transport-Security   removed on web05, web19          │
│    ⓘ 2 members share this difference. The golden peer may be the outlier.  │
│      [ make web05 the golden peer ]                                        │
│                                                                            │
│  No majority                                                               │
│    (none)                                                                  │
│                                                                            │
│  Versions                                                                  │
│    1.24.0   40 members                                                     │
│    1.22.1    1 member    web19                                    [ open ] │
└────────────────────────────────────────────────────────────────────────────┘
```

**Version drift is a separate block**, deliberately. "Someone patched 40 of 41" and "someone changed a location block" are different questions with different owners, and merging them buries both.

`make web05 the golden peer` is offered but never automatic. nagipath surfaces the observation; the designation is the operator's.

**Cluster of one** — Explicit note: `This cluster has one member, so drift is measured against its previous snapshot rather than against peers.` Never a clean green "no drift", which would be agreement with itself reported as a result.

**No Golden Peer, ≥3 members** — Majority baseline, stated. `no majority` fields list every variant with member counts; no plurality winner is invented.

---

## 4. `/instances/{id}/drift`

All three Baselines as tabs, side by side, because the temporal and spatial questions are genuinely different and operators ask both:

| Tab | Question |
|---|---|
| Previous snapshot | Did anything change here? |
| Golden peer | Does this match the reference? |
| Cluster majority | Does this match what most do? |

Each tab states its Baseline with a timestamp or member name, and shows `no baseline available` with the reason where it does not apply — rather than an empty success state, which reads as "clean" and is not.

---

## 5. Suggestions review

Reached from the banner. The screen that saves the feature.

```
┌────────────────────────────────────────────────────────────────────────────┐
│  Suggested ignore rules · web-prod                                         │
│                                                                            │
│  These are proposals. Nothing is suppressed until you accept it.           │
│                                                                            │
│  ☑  site name equals the host's own hostname                               │
│     object  site_name.name        pattern  {{node_hostname}}               │
│     would suppress 41 findings                                             │
│     observed: on every member this value equals that member's own hostname │
│     reason  [ per-host server_name; expected                            ]  │
│                                                          [ see examples ▾ ]│
│                                                                            │
│  ☑  listener address equals the host's primary IP                          │
│     would suppress 41 findings                                             │
│     reason  [ each member binds its own primary IP; expected by design  ]  │
│                                                                            │
│  ☐  access log path contains the hostname                                  │
│     would suppress 41 findings                                             │
│     reason  [                                                           ]  │
│     ⚠ a reason is required                                                  │
│                                                                            │
│  Accepting 2 rules would leave 20 findings.                                │
│                                     [ Cancel ]   [ Accept selected (2) ]   │
└────────────────────────────────────────────────────────────────────────────┘
```

Five decisions embedded here:

1. **Reasons are pre-filled but editable, and mandatory.** An ignore with no reason is technical debt that outlives whoever added it. Pre-filling removes the friction without removing the record.
2. **The observation is shown.** "On every member this value equals that member's own hostname" is why the operator can accept in two seconds instead of investigating.
3. **`would suppress N`** makes the blast radius explicit before committing.
4. **The running total** — `would leave 20 findings` — lets the operator see they are reaching a usable report.
5. **Nothing is pre-applied.** The banner said "suggested"; the screen says "proposals"; the button says "Accept selected". Auto-applying would be faster and would be the wrong shape for a tool whose credibility rests on not deciding things for you.

`see examples` expands three concrete findings the rule would suppress. Necessary — accepting a pattern that suppresses 41 findings unseen is not something a careful operator will do.

---

## 6. `/drift/ignore-rules` — Management

Table: object kind · field · pattern · reason · created by · created at · currently suppressing (count) · Delete.

Sortable by suppression count, so the rules doing the most hiding are easy to review. `Delete` restores those findings immediately, and the confirm names the count: `Deleting this rule will restore 41 findings.`

Empty state: `No ignore rules. Every difference is reported.` — a neutral statement of fact, not a prompt to add some.

---

## 7. Loading, error and empty states

| State | Treatment |
|---|---|
| Drift never computed | `Drift is computed after each collection. This instance has one snapshot, so there is nothing to compare yet.` Never "no drift". |
| No findings, genuinely | `No differences from <baseline>.` with the Baseline named and the computation time. Green. |
| No findings because everything is ignored | `No active differences. 41 findings are suppressed by 4 ignore rules.` with a link. The one presentation that must never look like a clean result. |
| Filtered to nothing | `No findings match these filters` + `Clear filters`. Distinct from having no findings. |
| Recomputing after a parser upgrade | `Recomputing after a parser update…` with a skeleton. The stale run is **not** displayed — a diff computed at a previous parser version can report differences our parser caused rather than the customer's changes, and showing it would be phantom drift. |
| Configuration unchanged since last collection | `No new snapshot since 2026-08-20; drift is unchanged.` Honest about why nothing is new. |
| Baseline Snapshot pruned by retention | `The previous baseline is no longer stored. Compared against 2026-07-14 instead.` The substitution is stated, never silent. |
| Degraded Snapshot on either side | Amber banner: `One side of this comparison was collected with gaps. Objects reported as removed may be missing from the collection rather than absent from the configuration.` The most important warning on the screen — it prevents a collection gap being escalated as a change. |
| Unparsed Snapshot on either side | `Cannot compare: configuration on web05 failed to parse.` with the error and a link to browse the text. Never an empty clean result. |
| Golden Peer retired | `The designated golden peer web02 is retired.` Falls back to majority, stated, with `[ choose a new golden peer ]`. |
| Mixed-vendor Cluster | Prevented at membership. If encountered in legacy data: `Cannot compare across vendors.` |
| 400 findings on first load | Suggestions banner first; findings collapsed by Cluster; the first group expanded. |
| `viewer` on ignore rules | Read-only list; create and delete disabled with the role hint. |

---

## 8. Acceptance criteria

| # | Criterion |
|---|---|
| 1 | "Violation", "desired state", "compliance" and any score appear nowhere. |
| 2 | The suggestions banner appears above findings whenever suggestions would suppress anything. |
| 3 | Every view names its Baseline in plain language. |
| 4 | Conforming counts are shown alongside diverging counts. |
| 5 | `reordered` findings carry a generated note that order is evaluation order. |
| 6 | A divergence on multiple members shows the Golden-Peer-outlier note and offers reassignment without performing it. |
| 7 | One divergence across many Instances is a single row listing them. |
| 8 | The ignored count is always visible beside the active count. |
| 9 | Both provenance links are present on every finding that has two sides. |
| 10 | Instance view offers all three Baselines as tabs, with reasons where one is unavailable. |
| 11 | Version drift is presented separately from configuration drift. |
| 12 | Suggestions show the observation, the suppression count, examples, and a pre-filled editable reason. |
| 13 | A reason is required; a blank reason blocks acceptance with an inline error. |
| 14 | Nothing is suppressed until explicitly accepted. |
| 15 | Ignore rules are listed with live suppression counts and are sortable by them. |
| 16 | Deleting an ignore rule states how many findings will return. |
| 17 | A stale drift run computed at an older parser version is never displayed. |
| 18 | A degraded Snapshot on either side warns that removals may be collection gaps. |
| 19 | An unparsed Snapshot yields an explicit failure, never a clean result. |
| 20 | A Cluster of one states that comparison is temporal, never reporting clean peer agreement. |
| 21 | Fields with no majority list every variant with counts; no plurality winner is chosen. |
| 22 | Baseline substitution after retention pruning is stated on screen. |
| 23 | Every filtered view is deep-linkable. |
