# Product: Fleet Inventory

**Pillar 2 of 4.** [PRD-V1 §3.2](../../PRD-V1.md). Backend: [node.md](../backend/node.md), [instance.md](../backend/instance.md), [config_object.md](../backend/config_object.md). UI: [../frontend/fleet_inventory.md](../frontend/fleet_inventory.md).
**Milestones:** M0 (Nodes) → M1/M2/M4 (per-Vendor parsing).

---

## 1. The problem

Nobody knows what web servers they have.

That sounds like an exaggeration until you ask. A typical enterprise answer is "about forty NGINX and some Apache", produced from an Ansible inventory that is between six months and three years stale, plus a spreadsheet. Nobody knows:

- Which versions, and therefore which CVEs apply. The answer is usually assembled per-host by hand during a security review.
- Which modules are compiled in. `nginx -V` output is not recorded anywhere, so "do we have `http_v2_module` everywhere" is a per-host investigation.
- How many Instances per host. Multi-instance hosts are common and consistently missed by tools that assume one web server per machine.
- Where the config actually lives. `-c` overrides, `--prefix` differences and distro packaging mean the assumption of `/etc/nginx/nginx.conf` is wrong often enough to matter.
- Which hostnames are served. Every `server_name` across the fleet is not written down anywhere.

The consequence: every security review, every capacity conversation, every migration plan starts with two weeks of discovery, done by hand, and is stale before it is finished.

---

## 2. What nagipath does

Point it at a host list. It discovers the Instances, asks each Vendor's own tooling what its configuration is, stores the answer, and parses it into queryable objects.

Then the questions that took two weeks take one query:

- Every Instance, Vendor, version and build flags across the fleet.
- Every hostname served, and by which Instance.
- Every Listener and port, including which are TLS.
- Every Upstream and its Members, with hostnames resolved from the Node's own point of view.
- Full-text search across current configuration — find every occurrence of a header, a backend, an IP.
- Configuration history, with the ability to see any file as it was on any past collection date.

### 2.1 The vendor tooling is the point

nagipath does not read `/etc/nginx/nginx.conf` and try to follow includes. It runs `nginx -T`, which is NGINX printing its own fully resolved configuration, every included file, in the order it actually uses them. Apache gets `-t -D DUMP_VHOSTS/DUMP_MODULES/DUMP_INCLUDES`. HAProxy has no `include` directive at all, so the `-f` arguments *are* the provably complete file set.

This is not a shortcut, it is the correctness argument. Every homegrown config collector eventually breaks on a wildcard include, a conditional include, or a distro's `conf.d` convention it has never seen. Asking the binary removes an entire class of wrongness — and it is the only approach that gets `-c` overrides and unusual prefixes right for free.

### 2.2 Multi-instance is a v1 requirement, not a v2 nicety

An Instance is the unit, not a Node. A host running three NGINX Instances with three different config roots is three Instances, discovered from `/proc/*/cmdline` and systemd units. Tools that model one web server per host are simply wrong on these hosts, and these hosts are common — they are how teams run a public and an internal edge on shared hardware.

Instances that are **stopped** are detected too, from systemd units with no matching process. A stopped web server is inventory, and often the more interesting kind.

### 2.3 History, not just current state

Every Collection stores a Snapshot. Configuration text is content-addressed and zstd-compressed, so forty near-identical Instances store one copy of each shared file, and a year of unchanged history costs almost nothing.

That makes "show me this file as it was in March" a real feature rather than a promise, and it is the foundation the Drift pillar sits on.

---

## 3. Journeys

### 3.1 Onboarding a fleet

1. Import an Ansible inventory, or paste a host list. **nagipath never scans networks** — Nodes come only from what the operator supplies. This is a deliberate constraint and it is the first question a security team asks.
2. Dry-run the import (the default) and see exactly what would be created.
3. Configure a default Credential. Approve host keys — each one explicitly, no trust-on-first-use.
4. Run a bulk Collection. Watch a live per-Node progress list.
5. Twenty minutes later there is a fleet inventory that did not exist that morning.

### 3.2 The security review

"Which Instances are running a version older than 1.24?" One filter. Previously: a spreadsheet and a week.

"Which have `http_ssl_module` missing?" One filter on build flags — a question that is essentially unanswerable by hand across forty hosts, because nobody records `nginx -V` output.

### 3.3 The hostname audit

"Every hostname this fleet serves." One page. Almost always reveals hostnames nobody expected: a decommissioned service still configured, a staging name on a production host, a wildcard broader than anyone remembers.

### 3.4 The migration plan

"Which Instances proxy to `legacy-app.corp.example`?" Full-text search plus the Upstream index. The list is the migration scope, derived rather than remembered.

---

## 4. Scope note on search

Full-text search covers **current configuration only**, not the full history.

The reason is concrete: FTS5 needs plaintext, and indexing every historical Snapshot multiplies the index by retention depth for a query nobody asked for. Historical text remains readable file by file through the Snapshot browser — what is not available is a fleet-wide historical grep.

This is stated in the API response as an unconditional `scope_note`, not left for the user to discover. PRD-V1 §3.2 originally promised search "across raw Snapshots"; that criterion has been corrected to match — see [schema.md §15](../backend/schema.md#15-findings-for-the-decision-record).

---

## 5. Acceptance criteria

| # | Criterion |
|---|---|
| 1 | Nodes are created only from operator input or imported inventory. No network scanning exists in the codebase. |
| 2 | Inventory import defaults to a dry run showing exactly what would be created. |
| 3 | Multiple Instances per Node are discovered and modelled independently. |
| 4 | Stopped Instances are discovered from unit files. |
| 5 | Configuration is captured via the Vendor's own tooling, with `-c` and prefix overrides honoured. |
| 6 | Version and build flags are recorded per Instance and are filterable fleet-wide. |
| 7 | Every hostname, Listener, Site, Route, Upstream and Upstream Member is queryable across the fleet. |
| 8 | Upstream hostnames are resolved on the owning Node; disagreements between Nodes are surfaced as findings. |
| 9 | Every derived object links to a file path, line and byte range in the stored Snapshot. |
| 10 | Full-text search covers current configuration, and every response says so. |
| 11 | Any stored file is viewable as it was at any retained Collection. |
| 12 | A partial capture is recorded as a **degraded** Snapshot with the specific failure, never silently as success. |
| 13 | An unparseable Snapshot is stored and reported with the parse error; the text remains browsable. |
| 14 | Identical files across Instances are stored once (content-addressed). |
| 15 | A 40-Node bulk Collection completes within 10 minutes at 8 concurrent workers. |

---

## 6. Success signals

**A design partner discovers an Instance they did not know they had.** Like the Trace signal, unfakeable — either the surprise is there or it is not, and in every fleet we have looked at, it is.

Supporting: number of Instances per Node above one (validates the multi-instance decision); percentage of Collections that complete non-degraded (our collection robustness, measured); count of unmodelled directive types weighted by frequency — literally our parser backlog, sorted by customer impact rather than by our guesses.

---

## 7. Risks

| Risk | Mitigation |
|---|---|
| `nginx -T` requires privilege the customer will not grant | A narrow, copy-pasteable sudoers grant covering only read-only inspection. Degraded capture where it is refused, with the specific command named. |
| Config in an unexpected location | Vendor tooling reports its own paths, so this is largely solved rather than guessed at. |
| Parse coverage gaps make inventory look incomplete | Opaque Directives are stored as Rules with `is_modelled=0` — the text is never lost, and the gap is measured and reported rather than hidden. |
| Fleet larger than the single-process design | Bounded and stated: hundreds of Nodes on one process. The HA door (`leased_until`) exists unused in the schema so scaling out later is a change, not a rewrite. |
| Operator expects agent-based continuous monitoring | Positioned clearly as scheduled agentless collection. Agentless is the reason it can be installed in an afternoon. |
