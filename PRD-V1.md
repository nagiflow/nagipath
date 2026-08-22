# nagipath v1 — Product Requirements

**Status:** operative. This is what is being built.
**Supersedes:** the scope, architecture, packaging, timeline and team assumptions in [PRD.md](PRD.md), which remains valid as market research.
**Vocabulary:** [CONTEXT.md](CONTEXT.md). Terms in Title Case are defined there and are used precisely.
**Locked decisions and their reasoning:** [docs/adr/](docs/adr/). **Deliberately unresolved:** [docs/OPEN-DECISIONS.md](docs/OPEN-DECISIONS.md).

## 1. What nagipath is

nagipath answers questions about a web server fleet that nobody can currently answer without logging into hosts one at a time:

- Where does a request for `payments.corp.example/api` actually go, hop by hop, and which layer is rewriting it?
- Which of these forty NGINX Instances has drifted from its peers, and in what?
- Which Certificates expire in the next thirty days, and which Sites break when they do?
- Which Rules apply to my Application's context path — asked by the developer who owns it, without a ticket?

It answers them by reading, over SSH, using each Vendor's own tooling as the authority on what its configuration means. It writes nothing to any managed host (ADR-0002).

**It is not** a configuration management tool, an orchestrator, a deployment pipeline, a monitoring system or a replacement for Ansible. It sits beside those and tells you what is true right now.

## 2. Who it is for

| | |
|---|---|
| **Buyer** | Infrastructure or platform engineering manager in a mid-to-large enterprise with 50–1,000 web server Nodes across at least two Vendors, and a segmented network that rules out vendor-cloud management planes. |
| **Primary user** | The reverse-proxy admin or SRE who owns the fleet, is the single point of failure for every routing question, and is the person paged at 3am. |
| **Secondary user (v1 read-only, no login)** | The application developer who wants to know which Rules touch their context path. In v1 the admin answers this from nagipath in seconds instead of an hour; developers logging in themselves is v1.1 and needs LDAP (see OPEN-DECISIONS). |

The wedge, on a first call, is one screen: a Trace of the customer's own Entry Point, showing a hop they did not know existed.

## 3. The four pillars

They are co-equal. They are affordable together because they are four views over one spine — collect → Snapshot → parse → store — which is roughly 80% of the engineering. Each pillar is mostly a query and a page.

### 3.1 Request-path trace

Given an Entry Point, produce the ordered Trace of Hops across the fleet, and say where it leaves.

**Acceptance criteria**
- Given a hostname and path, nagipath resolves the receiving Listener and Site, matches the Route, resolves the Upstream, and continues to the next Instance if an Upstream Member is a managed Instance.
- Route matching follows each Vendor's real precedence, not file order — NGINX `=` exact, then `^~` prefix, then regex in file order, then longest prefix; Apache `<Directory>` shortest-to-longest, then `DirectoryMatch`, then `<Files>`, then `<Location>`, with `RewriteRule` inheritance accounted for.
- Every Hop shows the Rules that fired on it, with their Action Class and a link to the exact bytes in the Snapshot (Provenance).
- A Trace that reaches an unmanaged destination ends in an External Hop that names the address and says why it stopped. It never ends silently and never guesses.
- Upstream Member hostnames are resolved on the target Node, not by the nagipath process (ADR-0010). Disagreement between two Nodes about the same name is surfaced as a finding, not averaged away.
- Every Hop and edge is labelled Inferred until verification says otherwise. No screen ever shows an unlabelled claim.
- Rendered as a graph, with a table beside it for people who want to copy text.

**Rule lookup by context path** ships with this pillar: enter a hostname and path prefix, get the ordered list of Candidate Rules across every Instance that could serve it, grouped by Instance and Action Class.

### 3.2 Fleet inventory

**Acceptance criteria**
- Nodes come from an operator-supplied list or an imported Ansible inventory (INI and YAML). nagipath never scans a network or a CIDR range (ADR-0002).
- Instances are detected from the process table (`/proc/*/cmdline`) and cross-referenced with systemd units, so multiple Instances of the same Vendor on one Node are found and distinguished from v1.
- For each Instance: Vendor, version, build flags and loaded modules, config root, Listeners, Sites, Routes, Upstreams, Certificate Bindings.
- Effective Configuration comes from the Vendor's tooling — `nginx -T`, `httpd -t -D DUMP_VHOSTS/DUMP_MODULES/DUMP_INCLUDES`, `haproxy -c` — never from our own include resolver (ADR-0004). Where the tooling is unavailable, results are labelled degraded rather than silently guessed.
- Every list is filterable and every row leads to the configuration text it came from.
- Full-text search across the **current** configuration of every Instance, for the questions the model does not anticipate. Historical Snapshots are retained and browsable but are not full-text indexed: indexing all history would store an uncompressed copy of every configuration file ever collected, which defeats content-addressed storage. Every search result states this scope.

### 3.3 Drift

**Acceptance criteria**
- Drift is reported against a Baseline that is either the Instance's previous Snapshot or its Cluster's Golden Peer (or the Cluster majority where no Golden Peer is designated).
- Clusters are discovered: Instances with identical configuration are one Cluster, and the operator renames it. Identical — not similar — is what keeps a discovered Cluster a safe drift baseline.
- A per-Cluster ignore list handles the legitimately host-specific differences — hostnames, bind addresses, instance IDs — because without it the report is noise on the first run and gets closed forever.
- Diffs re-parse both sides at the current parser version, so a parser upgrade can never manufacture phantom Drift (ADR-0012).
- Collection interval and Snapshot retention are configurable.
- Language is neutral throughout: Drift is a divergence, not a violation. nagipath has no opinion about which side is correct.

### 3.4 Certificate exposure

**Acceptance criteria**
- A Certificate is identified by fingerprint, so the same material on thirty Instances is one object with thirty Bindings — the actual question is "what breaks when this expires", and that needs the Bindings.
- Metadata (subject, SANs, issuer, validity, key size, signature algorithm, fingerprint) is extracted **on the target Node** via `openssl x509`. Certificate and key files are never transferred and never stored; Snapshots contain configuration files only (ADR-0009).
- HAProxy's Combined PEM convention is handled explicitly, since that is the case where naive collection would pull private keys.
- Expiry views by window, and a per-Application view answering "which of my Certificates expire soon".

## 4. Explicitly out of v1

Each of these is a deliberate cut, not an oversight. Triggers for reopening are in [docs/OPEN-DECISIONS.md](docs/OPEN-DECISIONS.md).

| Cut | Why |
|---|---|
| Any write to a managed host | Blast radius. Read-only is what gets a fleet tool installed (ADR-0002). |
| Editing — create and modify | The mechanism is designed (patch in place, ADR-0005) and Provenance is being built for it now; the create path is undecided. Modify-only was judged not worth shipping. |
| IIS and Windows | The single most expensive adapter: new OS, new transport, new config model, new lab. Headline v2 capability. |
| SSO / policy enforcement layers | The strongest differentiator and out of v1. PingAccess and friends come from admin REST APIs, not files over SSH. |
| Node agent, execution gateway | One process doing agentless SSH, behind one executor interface. |
| Control-plane HA, Postgres | Same piece of work, Enterprise-tier, customer-triggered (ADR-0003). |
| SaaS | A cloud control plane cannot dial into a segmented network, so a connector is installed either way (ADR-0001). |
| LDAP/AD, SSO login, Application-scoped RBAC | v1 is two roles — admin and viewer — global scope, local accounts. |
| Prometheus exporter, SIEM feeds, GitOps, Ansible/Puppet execution | Ansible inventory **import** ships; integrations do not. |
| Change orchestration, canary, batching, rollback | Downstream of writes. |
| Published scale numbers | Validated by local synthetic benchmark only. Publish nothing unmeasured. |

## 5. Architecture

One Go binary. `nagipath server` runs the control plane; `nagipath ctl` is the CLI. The UI is compiled in via `embed.FS`. Copy the binary, point it at a key file, run it.

```
┌─────────────────────── one binary, one process, inside the network ────────────────────────┐
│                                                                                            │
│   embedded UI              HTTP API            scheduler            Executor               │
│   html/template + htmx  ──▶ handlers  ──▶  worker pool, jitter ──▶  x/crypto/ssh ──┐       │
│   Cytoscape for the graph      │              next_run_at                          │       │
│                               ▼                                                    │       │
│                    SQLite (one file, WAL, STRICT, FTS5)                            │       │
│                    snapshots as zstd blobs · sqlc-typed queries                    │       │
└────────────────────────────────────────────────────────────────────────────────────┼───────┘
                                                                                     │ SSH
                                    ┌────────────────────────────────────────────────┘
                                    ▼
                              Nodes: nginx -T · httpd -D DUMP_VHOSTS · haproxy -c · openssl x509 · getent hosts
```

- **Datastore:** SQLite, and only SQLite — WAL, `STRICT` tables, FTS5 for search, `sqlc` for typed queries, Snapshots as zstd-compressed blobs in the database. No server, no port, no DBA, no install step. Backup is copying a file (ADR-0003).
- **UI:** server-rendered `html/template` plus htmx. JavaScript only for the Trace graph. Deliberately not an SPA (ADR-0013).
- **Transport:** in-process `golang.org/x/crypto/ssh`, because Credentials are entered in the web UI and stored encrypted — shelling out to `ssh` would mean writing private keys to disk on every connection (ADR-0011). Bastions are a field on a Node or Cluster; multi-hop by repetition. Key and SSH-certificate auth only.
- **Parsers:** position-preserving concrete syntax trees. Every Config Object and Rule carries `(file, byte range)` Provenance from the first parser onward, because retrofitting it later means rewriting them (ADR-0005).
- **Rules:** one uniform ordered record shape with a closed ~10-value Action Class taxonomy, not a typed schema per directive. Unmodelled directives land in the right scope and order with class `other`, so coverage is automatic and there is no schema treadmill (ADR-0006).
- **Identity:** natural keys per Config Object type with an ordinal tie-breaker. A rename reads as delete + create; that is accepted over fuzzy matching (ADR-0007).
- **Application:** a set of Entry Points. Everything else about it is derived by tracing. There is no membership table and adding one would defeat the point (ADR-0008).
- **Scheduler:** in-process, bounded worker pool, `next_run_at` plus jitter. An unused `leased_until` column is the door to multi-instance later.
- **Secrets:** Credentials encrypted AES-256-GCM under a Master Key read from a `0600` file (or env var) at startup. Missing or malformed key ⇒ refuse to start, never regenerate. Private key fields are write-only in API, UI and diagnostics bundle.
- **Privilege:** a narrow, copy-pasteable sudoers snippet covering only read-only inspection commands and reads of configuration and access-log paths. Nothing in it can mutate.

## 6. Verification: how a Trace becomes trustworthy

The four confidence states exist because "here are eleven Rules that might have matched" is not an answer an admin can use. Verification closes that gap.

1. The operator triggers a **Probe** — one GET or HEAD against an Entry Point. Explicitly per-probe, never scheduled, redirect-capped, audited with actor and target, and labelled with the originating host.
2. Response headers, status and redirect chain yield **Observed Effect** for the Rules whose result is visible.
3. nagipath then reads the access log on each candidate Instance and correlates the Probe's own request. Where a Hop is found in the log, it becomes **Verified**.

This works because we already parsed their `log_format` — we know how to read their logs because we read their config. The per-Vendor limits are real and must be stated in the UI rather than papered over:

| Vendor | Out of the box | Notes |
|---|---|---|
| HAProxy | `frontend/bind` and `backend/server` with `option httplog` | Strongest case; the log names the hop directly. |
| Apache | `%v` (Site) and `%f`, which shows `proxy:http://backend/...` | Good with common formats. |
| NGINX | Neither `$server_name` nor `$upstream_addr` is in stock `combined` | Verification requires a log-format change the customer must make. nagipath detects this, says so, and shows what to add — it does not make the change. |

Where a log cannot verify, the Trace stays Inferred and says why. An honest Inferred beats a confident wrong Verified.

## 7. Build order

One engineer, 10–20 hours a week, AI-assisted. Ordered by dependency, then by marginal cost. Weeks are a planning aid at ~15 h/week average, not a commitment.

| # | Milestone | ~wk | Why here |
|---|---|---|---|
| M0 | **Spine.** Binary skeleton, SQLite schema + sqlc, SSH Executor, Host Key Approval, encrypted Credentials, Node list + Ansible inventory import, Instance detection, Collection → Snapshot. docker-compose lab with all three Vendors. | 6 | Everything else is a view over this. |
| M1 | **NGINX adapter.** `nginx -T`/`-V` to Config Objects and Rules with Provenance. Inventory views live. | 5 | Most common, best tooling, cleanest grammar. |
| M2 | **HAProxy adapter.** No `include`, so the `-f` arguments are the complete file set. | 3 | Cheapest parser of the three, and it is the front tier — gives a genuinely two-tier Trace. |
| M3 | **Trace.** Entry Point → Hop chain, per-Vendor precedence, External Hop, remote DNS resolution, graph UI. | 5 | Gated on two Vendors. **First demoable build.** |
| M4 | **Apache adapter.** `DUMP_VHOSTS`/`DUMP_MODULES`/`DUMP_INCLUDES`, `<Directory>` precedence, RewriteRule inheritance. | 6 | Most expensive of the three; the Trace framework already exists to receive it. |
| M5 | **Drift.** Baselines, Clusters, Golden Peer, per-Cluster ignore list, scheduler, retention. | 4 | Nearly free once Snapshots and parsing exist. |
| M6 | **Certificates.** On-target metadata extraction, fingerprint identity, Bindings, expiry views. | 3 | Independent of Trace; can slot earlier if a design partner leads with it. |
| M7 | **Rule lookup by path.** Candidate ordering, Action Class grouping, full-text search. | 3 | The developer self-service surface. |
| M8 | **Probe and access-log correlation.** Observed Effect and Verified. | 4 | Needs every adapter's log_format parsing, so it comes last. |
| M9 | **Ship.** Packaging, license file, sudoers and install docs, hosted demo. | 4 | |

Total ≈ 43 weeks. The honest lever if that slips is **dropping a Vendor, not dropping verification or honesty labelling** — a tool that confidently shows the wrong Trace is worse than no tool. M0–M3 is the smallest thing worth showing anyone and is the milestone to protect.

## 8. Distribution and go-to-market

- **Closed source, no open-source tier.** Open-core does not help here: enterprise security approval gates installation regardless of licence, so a free tier buys reach that cannot be converted (ADR-0001, ADR-0014).
- **Licensing:** ed25519-signed offline file, verified against a key embedded in the binary. No phone-home — it would break air-gap operation and contradict the entire positioning. Enforcement is deliberately soft: warn loudly, never stop Collection (ADR-0014).
- **Trial:** a hosted demo seeded from the docker-compose lab. Synthetic data, no login, read-only, Collection hard-disabled by flag, reset on a schedule. It holds no customer data, so it carries no compliance burden. Operator-uploaded configuration is rejected for v1 — that would mean receiving other organisations' internal hostnames on an internet-facing host.
- **Pricing:** annual subscription by managed-Node band. Never per request, per Site or per Certificate — a control-plane buyer must never hesitate to discover an unmanaged server for fear of a bill. Numbers wait for design-partner conversations.
- **Content:** the category has no search volume; the problems do. Write for the queries people actually type — "nginx which location block matched request", "haproxy find which backend served request", "apache which vhost is serving this url", "find all expiring certs across servers".

## 9. Success signals for v1

Revenue is not the v1 metric; evidence that the Trace is the wedge is.

- Three design partners who ran a Collection against their own fleet.
- At least one Trace per partner that revealed a hop or a Rule the fleet owner did not know about. This is the single signal that matters — if it does not happen, the wedge is wrong.
- One partner's developers getting Rule answers from the admin's nagipath instead of a ticket.
- Time from binary download to first useful Trace, measured on a partner's real fleet, under one hour.
- Two partners naming IIS or the SSO layer unprompted as what they need next — that validates the v2 order rather than guessing it.

## 10. Risks

| Risk | Mitigation |
|---|---|
| **Parser fidelity.** A wrong Trace destroys trust faster than a missing feature. | Vendor tooling is the authority (ADR-0004); Opaque Directives are preserved and searchable rather than half-modelled; every claim carries its confidence state and its Provenance. |
| **The sudo grant is a deal-breaker.** Some security teams will not approve any privileged access for a new tool. | Grant is narrow, read-only, copy-pasteable, and documented line by line. Degraded operation without it is supported and labelled. |
| **NGINX access logs cannot verify without a customer change.** | Detect and state it in the UI with the exact directive to add. Never silently downgrade to a guess. |
| **Solo bus factor.** Nine or ten months part-time before anything ships. | M0–M3 is demoable at roughly week 19 and is the gate: if no design partner reacts to a Trace of their own fleet, stop and rethink before building M4–M9. |
| **Employer IP boundary.** | Never build or test against the employer's infrastructure, configurations or data. The docker-compose lab and synthetic fleets exist for this reason. |
| **Editing pressure arrives early.** Read-only will be called incomplete on the first call. | Position it as deliberate. Provenance and patch-in-place are already designed, so the answer is "next", not "we cannot". |
