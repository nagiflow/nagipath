# Open decisions

Decisions deliberately left open, with the trigger that should force each one. Anything not listed here and not covered by an ADR was not considered — treat it as a gap, not as a default.

## Editing mechanism (v2)

Editing must support **create and modify**, not modify-only — a modify-only editor was judged not worth shipping. The mechanism is undecided:

- **How new objects are emitted.** Candidate: create-by-clone, where the operator nominates an existing Site or Upstream as the exemplar, its bytes are copied verbatim out of the Snapshot, and only the differing fields are patched — so the generated text is in the customer's own style because it literally is their text. Alternative: a template library we author and maintain per Vendor.
- **Where new objects are placed.** Candidates: append into the exemplar's file; a per-Cluster file-naming pattern configured once; operator picks the target file at create time.

Modification itself is settled — patch in place, see ADR-0005. Only creation is open.

**Trigger:** before any write path is designed. ADR-0005's Provenance requirement holds regardless and is already in effect.

## Pricing

Model is settled in principle — annual subscription by managed-Node band, never per request, per Site or per Certificate, because a control-plane buyer must never hesitate to discover an unmanaged server for fear of a surprise bill. Actual numbers are undecided.

**Trigger:** design-partner willingness-to-pay conversations. Do not set a number before them.

## Postgres and control-plane HA

SQLite is the only datastore (ADR-0003). Postgres and a multi-instance control plane are the same piece of work, priced as an Enterprise-tier feature.

**Trigger:** a customer contractually requiring an HA management plane. Not before — and note the same port unlocks the SaaS door from ADR-0001.

## Execution gateway and node agent

v1 is one process doing agentless SSH from inside the customer's network, behind a single executor interface.

**Triggers, independently:**
- **Gateway** (outbound-connecting, per network zone): a design partner whose segmentation prevents one host from reaching the whole fleet.
- **Agent** (per Node): someone asking for sub-five-minute Drift detection by name. Polling with a configurable interval is the v1 answer.
- Either trigger also brings the per-zone Credential scoping that ADR-0011's resolution order defers.

## SSO / policy enforcement layer (v2)

Access-management layers are the strongest differentiator and are out of v1. Two structurally different shapes must both be modelled, because the same product appears as both: a **Hop in the Trace** (PingAccess in gateway mode, F5 APM, WebSEAL) and **policy attached to an existing Hop** (a PingAccess agent module inside Apache/IIS/NGINX, a SiteMinder web agent, Oracle OAM WebGate, `mod_auth_openidc`). Undecided:

- Which products v2 covers. Prevalence order to validate: PingAccess/PingFederate, SiteMinder, Oracle OAM WebGate, F5 APM, IBM Verify Access, Entra ID Application Proxy, then the OSS end.
- Whether detection alone is sufficient for most of them. Cheap detection — "an enforcement point is present, and it is this product" — may deliver most of the value without modelling any policy, since operators mostly need to know a hop exists rather than to audit its rules.

**Consequence already known:** PingAccess configuration comes from its admin REST API, not from files over SSH. The Collector/Parser interface split that would make an API-based collector a first-class citizen was considered for v1 and declined; it is therefore a known refactor rather than a new file.

## IIS and Windows

v1 covers NGINX, Apache and HAProxy. IIS is deferred as the single most expensive adapter — a new operating system, a new transport (WinRM/PowerShell), a new configuration model (XML, AppCmd, Shared Configuration) and a new test lab. It is the headline v2 capability and the reason for a second customer conversation.

## Developer access and RBAC depth

v1 has two roles, admin and viewer, global scope, local accounts. Rule-lookup-by-path ships in v1; opening it to a developer audience does not, because hand-creating accounts for hundreds of developers is not viable.

**Trigger:** wanting developers to log in. That requires LDAP/AD with group-to-role mapping plus Application-scoped viewer permissions, which is the well-defined v1.1. The permission check lives behind a single chokepoint function so scoping becomes a filter in one place rather than an audit of every query.

## Scale qualification

No public scale claim is made. Validation is by local synthetic benchmark — cloned lab containers and generated rows — with network latency deliberately excluded for now.

**Trigger:** a prospect asking for a number. Publish only what has been measured.
