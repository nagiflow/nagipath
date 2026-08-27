# nagipath — Specification Suite

A vendor-neutral control plane for web server fleets. It reads NGINX, Apache and HAProxy configuration across a fleet, builds one queryable model of it, and answers questions that no per-host tool can: **where does this request actually go, which rule actually wins, what breaks when this certificate expires, and how do these servers differ.**

v1 is read-only. nagipath writes nothing to any managed host.

---

## Read in this order

**New to the project?** [`../CONTEXT.md`](../CONTEXT.md) → [`../PRD-V1.md`](../PRD-V1.md) → [`infra/architecture.md`](infra/architecture.md) → [`backend/schema.md`](backend/schema.md).

**Implementing a feature?** Its product spec (why, and what "done" means) → its backend spec (data and API) → its frontend spec (screens and states).

**Wondering why something is the way it is?** [`adr/`](adr/) first. Every non-obvious decision has one.

---

## Root documents

| Document | What it is |
|---|---|
| [`../CONTEXT.md`](../CONTEXT.md) | **The glossary.** Canonical domain vocabulary. Node, Instance, Site, Location, Rule, Trace, Hop, Snapshot, Application, Entry Point. No implementation detail. |
| [`../PRD-V1.md`](../PRD-V1.md) | **The operative build plan.** Scope, milestones M0–M9, acceptance criteria. What is actually being built. |
| [`../PRD.md`](../PRD.md) | Market and competitive research. Predates the build decisions and is bannered as such — historical, not operative. |
| [`OPEN-DECISIONS.md`](OPEN-DECISIONS.md) | Questions deliberately left open, with the deadline each must be answered by. |

---

## Architecture decisions

Each records a decision that is hard to reverse, surprising without context, and the result of a real trade-off.

| ADR | Decision |
|---|---|
| [0001](adr/0001-self-managed-only-saas-deferred.md) | Self-managed first, SaaS deferred — one binary serves both |
| [0002](adr/0002-read-only-v1.md) | v1 is read-only |
| [0003](adr/0003-sqlite-single-file-datastore.md) | Embedded SQLite as the only datastore |
| [0004](adr/0004-vendor-tooling-is-the-config-authority.md) | Vendor tooling is the configuration authority, not our file walk |
| [0005](adr/0005-position-preserving-parse-and-patch-in-place.md) | Position-preserving parse, patch in place |
| [0006](adr/0006-uniform-rule-records-not-typed-schemas.md) | Uniform Rule records with a closed Action Class enum |
| [0007](adr/0007-natural-key-object-identity.md) | Natural-key object identity |
| [0008](adr/0008-application-is-a-set-of-entry-points.md) | An Application is a set of Entry Points, never a list of servers |
| [0009](adr/0009-never-transfer-private-key-material.md) | Never transfer private key material |
| [0010](adr/0010-resolve-dns-on-the-target-host.md) | Resolve DNS on the target host |
| [0011](adr/0011-in-process-ssh-with-stored-credentials.md) | In-process SSH with stored credentials |
| [0012](adr/0012-parser-version-and-reparse-policy.md) | Parser version and reparse policy |
| [0013](adr/0013-server-rendered-ui.md) | Server-rendered UI with htmx |
| [0014](adr/0014-offline-license-with-soft-enforcement.md) | Offline licence with soft enforcement |
| [0015](adr/0015-traces-are-not-pinned-retention-marks-them-unreproducible.md) | Retention never pins Snapshots for a Trace; affected Traces become unreproducible |

---

## Infrastructure

| Document | Covers |
|---|---|
| [`infra/architecture.md`](infra/architecture.md) | Process model, packages, the SSH executor, the scheduler, the two connection pools, the request lifecycle |
| [`infra/customer_deployment.md`](infra/customer_deployment.md) | Install, systemd, TLS, backup, upgrade, sizing, observability, the security-review answer sheet, failure modes |
| [`infra/release_and_demo.md`](infra/release_and_demo.md) | Build pipeline, signing and SBOMs, the hosted demo, offline licence issuance, what we deliberately do not build |
| [`infra/test_lab.md`](infra/test_lab.md) | The fixture corpus, the container lab, differential testing against vendor tooling, non-functional tests |
| [`infra/development.md`](infra/development.md) | Local Docker development with Air hot reload |

---

## Backend

[`backend/schema.md`](backend/schema.md) is authoritative for all DDL. The entity documents reference it rather than restating it. [`backend/api_conventions.md`](backend/api_conventions.md) covers pagination, errors, filtering and auth, and every API section assumes it.

| Document | Entities and concerns |
|---|---|
| [`schema.md`](backend/schema.md) | **All DDL.** Every table, index, constraint, the derived-row contract, plus §15 open findings |
| [`api_conventions.md`](backend/api_conventions.md) | Cursor pagination, error envelope, filter syntax, authentication, idempotency |
| [`node.md`](backend/node.md) | Node, Cluster, host key approval, Ansible inventory import, quarantine |
| [`credential.md`](backend/credential.md) | AES-256-GCM at rest, AAD binding, the Master Key, the three-tier resolution order |
| [`instance.md`](backend/instance.md) | Instance discovery, vendor and version detection, multi-instance hosts, the config authority commands |
| [`collection.md`](backend/collection.md) | The collection state machine, content-addressed zstd blobs, Snapshots, degraded outcomes, retention |
| [`config_object.md`](backend/config_object.md) | Site, Listener, Location, Upstream, Upstream Member — the derived model and its provenance |
| [`rule.md`](backend/rule.md) | Uniform Rules, Action Classes, `precedence_rank` and `specificity`, inheritance, shadowing, FTS |
| [`application.md`](backend/application.md) | Application, Entry Point, footprint derivation, the reverse query |
| [`trace.md`](backend/trace.md) | Trace and Hop, the resolution algorithm, terminal reasons, External Hops, path transformation |
| [`probe.md`](backend/probe.md) | Probe, token correlation, log-format capability by vendor, evidence, negative evidence |
| [`drift.md`](backend/drift.md) | Baseline selection, object-level comparison, ignore rules, version drift |
| [`certificate.md`](backend/certificate.md) | Fingerprint identity, Bindings, coverage findings, the renewal checklist, combined PEMs |
| [`user_license_audit.md`](backend/user_license_audit.md) | Users, sessions, API tokens, roles and the single authz chokepoint, audit, licence |

---

## Product

Why each pillar exists, who feels the problem, and the numbered criteria that define done.

| Document | Pillar |
|---|---|
| [`product/fleet_inventory.md`](product/fleet_inventory.md) | Know what you have, from vendor tooling rather than guesswork |
| [`product/request_path_trace.md`](product/request_path_trace.md) | Follow a request across vendors and hosts |
| [`product/trace_verification.md`](product/trace_verification.md) | Prove the trace with a real request and real log lines |
| [`product/rule_lookup_by_path.md`](product/rule_lookup_by_path.md) | Which rule actually wins, and why |
| [`product/drift_detection.md`](product/drift_detection.md) | How these servers differ, without asserting how they should be |
| [`product/certificate_exposure.md`](product/certificate_exposure.md) | What breaks when this expires, and where to change it |
| [`product/collection_scheduling_and_retention.md`](product/collection_scheduling_and_retention.md) | Staying current without one broken host poisoning everything |
| [`product/access_and_audit.md`](product/access_and_audit.md) | The security review's questions, each answered structurally |

---

## Frontend

[`frontend/design_system.md`](frontend/design_system.md) is assumed by every other frontend document. Each spec states what the operator sees, every control and what clicking it does, and its loading, empty and error states.

| Document | Screens |
|---|---|
| [`design_system.md`](frontend/design_system.md) | Confidence badges, provenance links, component inventory, htmx conventions, error classes, keyboard, accessibility |
| [`onboarding.md`](frontend/onboarding.md) | First run to first Trace in under 30 minutes |
| [`fleet_inventory.md`](frontend/fleet_inventory.md) | Fleet overview, Nodes, Instances, Snapshot browser, search |
| [`trace_explorer.md`](frontend/trace_explorer.md) | Hop cards, precedence explanations, shadowed rules, the graph, verification |
| [`rule_lookup.md`](frontend/rule_lookup.md) | Path lookup, precedence explanation, cross-instance comparison, coverage distribution |
| [`drift_report.md`](frontend/drift_report.md) | Fleet and cluster views, the suggestions flow, ignore-rule management |
| [`certificates.md`](frontend/certificates.md) | Expiry list, impact, coverage, renewal checklist, findings |
| [`applications.md`](frontend/applications.md) | Application list, creation, the derived footprint |
| [`settings.md`](frontend/settings.md) | Credentials, host keys, schedule, retention, users, tokens, audit, licence, diagnostics |

---

## Conventions in this suite

- `schema.md` is the single source of DDL. No other document restates a table definition.
- Every claim in the UI links to a file and line number. Provenance is a product feature, and the specs treat it as one.
- Confidence is `inferred` → `candidate` → `observed_effect` → `verified`; worst-of wins on any aggregate. Nothing is presented as more certain than its weakest input.
- Contradictions between documents are recorded as findings, not silently resolved. Live ones are in [`schema.md` §15](backend/schema.md) and [`OPEN-DECISIONS.md`](OPEN-DECISIONS.md).
- The words "violation" and "desired state" appear nowhere in the Drift feature. nagipath reports differences; the operator decides which are wrong.

---

## Findings raised and resolved

Three contradictions surfaced during specification. All three are now settled; the reasoning is kept because it is the part worth reading. Full text in [`schema.md` §15](backend/schema.md).

| Finding | Resolution | Where |
|---|---|---|
| Credential resolution has a hole: the Cluster tier cannot apply on first contact, and a Node whose Instances span two disagreeing Clusters falls through to the default. | Both cases documented in **ADR-0011's Consequences**, surfaced in the UI, and attributed to Cluster membership being derived rather than declared. No plurality winner is guessed. | ADR-0011, [`credential.md`](backend/credential.md) |
| `PRD-V1 §3.2` promised full-text search "across raw Snapshots". FTS covers **current** Snapshots only. | The acceptance criterion now says "current configuration" and states why. History stays retained and browsable, just not indexed. | [`../PRD-V1.md`](../PRD-V1.md) §3.2 |
| A Trace can outlive the Snapshots it walked, once retention prunes them. | **ADR-0015** — retention wins, nothing is pinned. Affected Traces become `unreproducible`: shape survives, per-Hop detail does not, `Retrace` recovers it, and Probe evidence is unaffected. The state is derived, never stored. | [ADR-0015](adr/0015-traces-are-not-pinned-retention-marks-them-unreproducible.md), [`trace.md` §5.1](backend/trace.md), [`schema.md` §14](backend/schema.md) |

Genuinely open questions live in [`OPEN-DECISIONS.md`](OPEN-DECISIONS.md), each with the trigger that should force it.
