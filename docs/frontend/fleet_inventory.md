# Fleet Inventory Screens

**Assumes:** [design_system.md](design_system.md). **Backend:** [node.md](../backend/node.md), [instance.md](../backend/instance.md), [collection.md](../backend/collection.md), [config_object.md](../backend/config_object.md).
**Product:** [../product/fleet_inventory.md](../product/fleet_inventory.md). **Milestone:** M0–M4.

---

## 1. Screens covered

| Route | Screen |
|---|---|
| `/fleet` | Fleet overview — the landing page after login |
| `/nodes`, `/nodes/{id}` | Node list and detail |
| `/instances`, `/instances/{id}` | Instance list and detail |
| `/sites`, `/routes`, `/upstreams` | Cross-fleet object browsers |
| `/snapshots/{id}`, `/snapshots/{id}/files/{fid}` | Snapshot browser and file viewer |
| `/search` | Full-text configuration search |
| `/collections`, `/collections/{id}` | Collection history and timeline |

---

## 2. `/fleet` — Overview

The landing page. It answers "is my fleet inventory healthy, and what needs my attention" in one screen, then gets out of the way.

```
┌────────────────────────────────────────────────────────────────────────────┐
│  Fleet                                             collected 2h ago        │
│                                                                            │
│  ┌────────────┐ ┌────────────┐ ┌────────────┐ ┌────────────┐               │
│  │   47       │ │   52       │ │   6        │ │   3        │               │
│  │   nodes    │ │  instances │ │  clusters  │ │ apps       │               │
│  │  44 ok     │ │  nginx 38  │ │            │ │            │               │
│  │  2 failing │ │  apache 9  │ │            │ │            │               │
│  │  1 quarant.│ │  haproxy 5 │ │            │ │            │               │
│  └────────────┘ └────────────┘ └────────────┘ └────────────┘               │
│                                                                            │
│  Needs attention                                                           │
│  ⚠  1 node quarantined            web12 — sudo: password required   [view] │
│  ⚠  2 nodes failing collection    same cause: auth failure          [view] │
│  ⚠  3 instances collected degraded openssl not found                [view] │
│  ⚠  1 certificate expires in 24d  payments.corp.example             [view] │
│  ⚠  4 drift findings              across 2 clusters                 [view] │
│                                                                            │
│  Trace a request                                                           │
│  [ https://______________________________________ ]        [ Trace ]       │
└────────────────────────────────────────────────────────────────────────────┘
```

**"Needs attention" is the whole point of this page** and it has three rules:

1. **Only actionable items.** No "47 nodes healthy" row. A dashboard that lists good news trains people to skim past the bad news.
2. **Grouped by cause, not by object.** "2 nodes failing — same cause: auth failure" is one line and one fix. Two separate rows would be two investigations of the same problem.
3. **Empty is a real state.** When nothing needs attention the block reads `Nothing needs attention. Last collection completed 2h ago with no failures.` — with the timestamp, because "nothing wrong" from a collector that stopped running three weeks ago is the most dangerous screen in any monitoring product.

The trace box is on the overview because it is the most-used action in the product and should never be more than one click away.

---

## 3. `/nodes` — Node list

Columns: Name · Address · Cluster · Instances · State · Last collected · Last failure.

Filters: state (ok / never checked / pending host key / failing / quarantined / retired), Cluster, Vendor present, source (manual / ansible), stale (no successful collection in N days).

**State is computed, never stored** — derived from host key approval, failure counters and last-success time. A stored state column would drift from reality, and the reality is cheap to compute.

Row actions on hover: `Check` · `Collect` · `Open`. Bulk selection enables `Check selected` and `Collect selected`.

**Bulk collect** opens the progress list (see §8) rather than blocking the page.

**Empty state** — `No nodes yet.` then: *nagipath never scans networks. Add a host list or import an Ansible inventory.* with both buttons. The scanning statement lives here permanently, not just in onboarding, because this is the page a security reviewer is shown.

**Filtered-to-empty** — `No nodes match these filters` with a `Clear filters` button. Distinct copy from the no-nodes state; conflating them makes an operator think their fleet vanished.

---

## 4. `/nodes/{id}` — Node detail

Header: hostname, address, port, Cluster, computed state badge, source chip (`manual` / `ansible`).

Actions: `Check` · `Collect` · `Edit` · `Retire` · `Purge` (admin only; `viewer` sees them disabled with a role hint).

Tabs:

**Instances** — Cards for each Instance: Vendor, version, config path, PID, systemd unit, state, and a `verification_capability` chip (`strong` / `good` / `needs log format change`). That chip is here rather than only on the Trace screen so the limitation is discoverable before someone runs a Probe and is disappointed.

**Connection** — Credential in use and **which resolution tier chose it** (`per-node` / `per-cluster` / `default`). When two Clusters conflict and it fell back to the default, that is stated as a warning here — it is the documented hole in the resolution order and hiding it would make a real ambiguity invisible.

Host key block: type, fingerprint, approval date, approver. `Re-approve` if changed.

**Collections** — History table: time, outcome (success / degraded / failed), duration, files, size, and the step that failed. Row opens the timeline.

**Facts** — OS, kernel, architecture, package manager, `getent hosts` availability, `openssl` availability. Small, but it is what turns "collection is degraded" into "this host has no openssl".

### 4.1 Retire vs purge

Two different actions, and the dialogs make the difference unmistakable:

- **Retire** — `Stop collecting from web12. Its 34 snapshots, traces and history are kept and remain searchable. This can be undone.` Single confirm.
- **Purge** — `Permanently delete web12 and all of its data: 34 snapshots, 1,890 stored files, 12 traces, 3 drift runs. Audit events are kept. This cannot be undone.` Typed hostname confirmation.

Purge names the exact counts, because "and all associated data" is a phrase people click through without reading.

---

## 5. `/instances` — Instance list

The security review's screen, and the one that justifies the multi-instance decision.

Columns: Node · Vendor · Version · State · Sites · Routes · Upstreams · Rules · Config path · Last collected.

Filters: Vendor, version (with `<` and `>` comparison — the CVE question), state (running / stopped / retired), build flag contains, Cluster, degraded last collection, `is_modelled=0` count above a threshold.

The version comparison filter is worth calling out: "every Instance older than 1.24" is the question asked in every security review, and a text-equality filter cannot answer it.

Grouped-by-Node toggle makes multi-instance hosts obvious at a glance — a host with three Instances is the case competing tools get wrong, and showing it plainly is quietly persuasive.

---

## 6. `/instances/{id}` — Instance detail

Header: Node, Vendor, version, state, config path, last collected, verification capability.

Actions: `Collect` · `Re-parse` · `Compare with peer` · `Open current snapshot`.

Tabs:

**Topology** — Listeners → Sites → Routes → Upstreams as a nested, collapsible tree. Every node in the tree carries a provenance link. Routes are shown **in evaluation order** with their precedence explanation, because that is the order that matters and alphabetical would actively mislead.

**Rules** — Table grouped by Action Class, filterable, with an `unmodelled only` toggle. Unmodelled Rules show verbatim text and the note that nagipath stores but does not interpret them.

**Build** — Version, configure arguments, module list, one module per row and filterable. This is `nginx -V` output made queryable, which is data that exists nowhere else in most organisations.

**Configuration** — The Snapshot file tree, with the entry file marked and included files nested in the order the Vendor reported.

**Drift** — Findings for this Instance across all three Baselines ([drift_report.md](drift_report.md)).

**Applications** — Every Application whose Traces cross this Instance. The pre-decommission question, and the reverse query a hand-maintained CMDB answers confidently and wrongly.

**Certificates** — Bindings on this Instance with expiry.

---

## 6a. Clusters — on `/fleet`, not a screen of their own

A Cluster is discovered, so there is nothing to create and no membership to manage; there is therefore no Cluster screen. It appears as the level above the node in the fleet table — cluster → node → instance, read top to bottom — with the nodes that are in none grouped last under `in no cluster`.

The cluster row carries the name, the member count, the golden peer if one is set, a link to that Cluster's Drift report, and (admin, folded away) the rename form. Renaming is the only cluster action in the product.

Discovery runs when the fleet is opened, so the grouping is always current with the last Collection without a scheduler owning it. `instances with identical configuration` is stated on the row, because it is the whole definition: nothing was guessed and nothing was declared. See [instance.md §4.2](../backend/instance.md) for why identity — not similarity — is what makes a discovered Cluster a safe Drift baseline.

---

## 7. `/snapshots/{id}` and the file viewer

**Snapshot browser** — File tree with sizes, the entry file marked, degraded gaps shown as `not captured — permission denied` entries rather than as absences. An absent file and an unreadable file look identical if you only show what you have, and that difference matters a great deal.

Header: collection time, parser version, outcome, total files, total bytes, and `Compare with…` (a Snapshot picker for the same Instance).

**File viewer** — Monospace, line-numbered, byte-range highlighted when arrived at from a provenance link, with `Copy path`, `Copy contents`, `Download` and `View at another collection`.

Above the content, a banner when the Snapshot is not current: `This is the configuration as collected on 2026-03-04. The current configuration differs.` with `[ View current ]`. Reading a historical file believing it to be current is a genuine hazard, and the banner is the only thing preventing it.

**Diff view** — Side-by-side above 1400px, unified below. Toggle between structured findings (default) and text diff. Structured is default because a text diff of two Apache configs differing in whitespace is noise; text is available because sometimes the bytes are the question.

---

## 8. Collection progress list

Used from Node detail, bulk collect and onboarding. htmx polls every 3s and **stops polling when the run completes** — a page that polls forever holds a connection forever.

```
  Collecting 41 nodes                                     28 done · 2 failed
  ▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓░░░░░░░░░░

  ✓  web01    nginx 1.24.0 · 62 files · 4.1s
  ⚠  web07    degraded — openssl not found; certificate metadata skipped ▾
  ✗  web12    failed — sudo: a password is required for 'nginx -T'      ▾
  ⟳  web13    reading configuration…
  ○  web14    queued
```

Expanded failure row shows the exact command, its stderr, and the fix — for the sudo case, the copy-pasteable sudoers snippet. This one expansion converts the most common onboarding failure from a support ticket into a paste, and it is the highest-leverage text in the product.

`Collect all` / `Retry failed` at the bottom. `Retry failed` retries only the failures, which is what an operator wants after fixing one shared cause.

---

## 9. `/search` — Configuration search

Single input, results grouped by Instance. Each hit: file path, line number, the matching line with the term highlighted, and two lines of context. Click opens the file viewer at that line.

Filters: Vendor, Cluster, Instance, file path pattern.

**Above the results, unconditionally:**

> Search covers the current configuration of every instance. Historical snapshots are browsable file by file but are not indexed for search.

Unconditional because a caveat that appears only sometimes is a caveat nobody learns. PRD-V1 §3.2's wording has been corrected to match this scope — see [schema.md §15](../backend/schema.md#15-findings-for-the-decision-record).

The tokenizer splits on `/ . : - _`, so `proxy_pass`, `10.0.0.1`, `/api/v2` and `X-Forwarded-For` all work as typed. An example row under the input shows those four, because search that silently fails on the queries an operator would naturally type reads as a broken index.

---

## 10. Cross-fleet browsers

`/sites` — every hostname served, fleet-wide. Columns: hostname · Instance · Node · Vendor · Listener · TLS · default/catch-all. Filterable by hostname substring. Almost always reveals hostnames nobody expected, which is why it is a top-level route rather than buried in Instance detail.

`/routes` — every Route, with `ordered=true` grouping by Site in evaluation order with precedence explanations.

`/upstreams` — every Upstream and Member, with resolved addresses and the resolving Node. A `disagreements` filter shows names that resolve differently from different Nodes — a finding, not an error, and one nobody can find by hand.

---

## 11. Error and empty states

| State | Treatment |
|---|---|
| Node never checked | `Host key not yet approved.` `[ Check now ]`. Not an error — the expected state after creation. |
| Node pending host key approval | Fingerprint shown with `[ Approve ]`. |
| Node quarantined | Amber banner: consecutive failure count, last failure reason, last successful collection date, `[ Collect now ]` which clears quarantine on success. |
| Node retired | Grey, read-only, historical data intact, `[ Unretire ]`. |
| Instance with no successful collection | `No configuration collected yet` and the last failure. Never an empty topology tree, which would read as "this server has no configuration". |
| Snapshot degraded | Amber chip fleet-wide on any view of its data, with the specific gap. |
| Snapshot unparsed | Topology tab shows the parse error and links to browse the text. Text is always available even when parsing failed. |
| Instance stopped | `stopped` chip; last known configuration shown with the date. A stopped server is inventory. |
| Collection in progress | Existing data stays visible with an inline `collecting…` chip. Never replaced by a spinner. |
| Search with no results | `No matches in current configuration` plus the scope note and a reminder that historical text is not indexed. |
| Search term shorter than 2 characters | Inline: `Enter at least 2 characters.` |
| Provenance link to a pruned Snapshot | `This configuration is no longer stored (retention).` with the Snapshot's collection date and a link to the current file. |
| Mixed-vendor Cluster | Impossible: a Cluster is identical configuration, and two Vendors never produce identical configuration. |
| Licence node limit exceeded | Banner naming count and limit. Node creation still works. Never a hard stop. |

---

## 12. Acceptance criteria

| # | Criterion |
|---|---|
| 1 | Overview lists only actionable items, grouped by cause. |
| 2 | "Nothing needs attention" always shows the last collection time. |
| 3 | Node state is computed from real data, never stored. |
| 4 | Node list empty state states that nagipath never scans networks. |
| 5 | No-results and no-data empty states have distinct copy. |
| 6 | Purge confirmation names exact counts and requires typed confirmation; retire is a single confirm and reversible. |
| 7 | Credential resolution tier is displayed, including the fallback warning when Clusters conflict. |
| 8 | Verification capability is shown on the Instance, not only in the Trace. |
| 9 | Instance list supports version comparison filtering, not just equality. |
| 10 | Multi-instance Nodes are visible via grouping. |
| 11 | Routes are always displayed in evaluation order with precedence explanations. |
| 12 | Build flags and modules are individually filterable. |
| 13 | Every derived object has a provenance link opening the file viewer at the byte range. |
| 14 | A non-current file view carries a banner stating the collection date and offering the current version. |
| 15 | Degraded Snapshots show uncaptured files as explicit entries, never as absences. |
| 16 | An unparsed Snapshot still allows text browsing. |
| 17 | Collection progress shows per-Node live results, expandable to the failed command and its fix. |
| 18 | Progress polling stops when the run completes. |
| 19 | `Retry failed` retries only failures. |
| 20 | Search states its current-only scope unconditionally and shows tokenizer examples. |
| 21 | Upstream browser surfaces cross-Node DNS disagreements. |
| 22 | Every filtered view is deep-linkable via the URL. |
| 23 | Pagination is cursor-based with `Load more`; no page numbers anywhere. |
