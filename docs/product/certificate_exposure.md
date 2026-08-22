# Product: Certificate Exposure

**Pillar 4 of 4.** [PRD-V1 §3.4](../../PRD-V1.md). Backend: [certificate.md](../backend/certificate.md). UI: [../frontend/certificates.md](../frontend/certificates.md).
**Milestone:** M6.

---

## 1. The problem

Certificates expire, and organisations find out from customers.

The usual state of affairs: certificates are tracked in a spreadsheet that covers the ones somebody remembered. Monitoring, where it exists, checks the public endpoints — which misses every internal certificate, every certificate on a backend hop, and every certificate on a hostname that is served but not monitored. Renewal is a per-certificate scramble because nobody knows how many places the certificate is deployed.

The question that is genuinely hard is not "when does this expire". A dozen tools answer that. It is:

**"What breaks when this expires?"**

Answering it requires knowing every Listener and Site that serves this exact certificate, across every Instance, plus which Applications depend on those paths. That is a fleet-wide join nobody can do by hand, and it is why renewals are discovered to be incomplete after the fact — one of the eleven deployments was missed.

There is also a quieter problem: **internal TLS that does not verify.** `proxy_ssl_verify off`, `SSLProxyVerify none`, HAProxy `verify none`. Extremely common, almost never known, and it means the encrypted hop to the backend is encrypted against nobody in particular.

---

## 2. What nagipath does

Because it has already parsed every configuration, it knows every certificate path that is actually referenced. It extracts metadata for each one **on the target host** and models certificates by fingerprint, so the same material found in twelve places is one Certificate with twelve Bindings.

Then:

- Expiry across the whole fleet, including internal certificates no external monitor can see.
- **Blast radius per certificate** — every Binding, Instance, Node, Site and Application.
- **Hostname coverage mismatches** — a Site serving a name the certificate does not cover.
- Weak configuration: keys under 2048 bits, SHA-1 signatures, self-signed certificates on public-looking names, validity periods over 398 days.
- Unverified backend TLS.
- A **renewal checklist**: what to change, grouped by Node, with each Vendor's reload requirement.

### 2.1 Private key material never moves

Metadata is extracted on the host with `openssl x509`. Only the parsed output crosses the SSH connection. There is no column for a private key anywhere in the schema, and there must never be one.

HAProxy is why this is enforced in the transport layer rather than trusted to each adapter. HAProxy's `crt` convention puts certificate, chain **and private key** in one file — so any collector that naively fetches "the certificate file" pulls a production load balancer's private key across the network and writes it to a database. That is the *default* configuration shape for one of the three v1 Vendors, not a hypothetical.

Where `openssl` is absent, `PRIVATE KEY` blocks are stripped in memory before any buffer is passed onward. Snapshots contain configuration files only, never certificate or key files.

This is also a sales argument. "Your private keys never leave your servers" is a sentence that gets a security review approved.

### 2.2 Fingerprint identity is the feature

Thirty near-identical rows for one certificate deployed thirty times is an inventory. One row with thirty Bindings is an answer. The operator's question is about the certificate, so the certificate is the object.

### 2.3 Only what is served

A certificate sitting in `/etc/ssl` that no directive references is not collected. nagipath reports what is **served**. A stray file is a housekeeping matter, and inventorying it would dilute the list that matters — which is the difference between a report people act on and a report people skim.

Similarly, intermediates are excluded from expiry lists by default. An intermediate expiring in eight years is not a finding, and mixing it in trains people to stop reading.

---

## 3. Journeys

### 3.1 Twenty-four days out

Dashboard shows one certificate under 30 days. Open it: 12 Bindings, 6 Nodes, 2 Applications, and 4 of the files are Combined PEMs that also contain private keys — which changes who has to do the work and under what change control. The renewal checklist is grouped by Node with the reload command per Vendor.

That is a change ticket, produced in ninety seconds, that is *complete* — which is the property the spreadsheet never had.

### 3.2 The coverage finding

`api.corp.example` is served by a certificate covering neither it nor a matching wildcard. Clients get a name mismatch. This is cheap for us to compute and genuinely hard for a customer to compute across a fleet, which makes it disproportionately effective in a demo.

### 3.3 The expired certificate nobody noticed

An internal certificate, expired six weeks ago, still bound to a Listener. No external monitor watches that hostname. Frequently the thing that makes someone install the tool in the first place.

### 3.4 The unverified hop

Three Upstreams proxy over HTTPS with verification disabled. Nobody chose that; it was copied from a working example years ago. Reported factually, with the directive and its line.

### 3.5 The Application owner's view

"Which of my certificates expire soon" — answered for someone who does not know or care which Instances serve them, derived from their Application's Traces.

---

## 4. Observations, not scores

Weak-configuration findings are reported with their reason and never as a grade. A letter grade invites arguing with the grade instead of fixing the certificate, and a self-signed certificate on an internal Site is a legitimate choice — calling it a violation is how a report loses credibility with the person who made that choice deliberately.

---

## 5. Acceptance criteria

| # | Criterion |
|---|---|
| 1 | No private key material is ever transferred or stored. No schema column exists for it. |
| 2 | Metadata is extracted on the target host; where `openssl` is absent, key blocks are stripped in memory before any buffer is passed on. |
| 3 | Snapshots never contain certificate or key files. |
| 4 | Certificates are identified by SHA-256 fingerprint; one certificate deployed many times is one object with many Bindings. |
| 5 | Every Binding names the Instance, Site, Listener, file path and provenance line. |
| 6 | Combined PEM files are flagged at both the Binding and the Certificate level. |
| 7 | Expiry is computed at read time, never stored. |
| 8 | Blast radius per certificate lists Bindings, Instances, Nodes, Sites and Applications. |
| 9 | Hostname coverage is checked against every Site name, honouring wildcard semantics, reporting `name_not_covered`, `wildcard_only` and `extra_sans`. |
| 10 | Weak configuration is reported as observations with reasons, never as a score or grade. |
| 11 | Unverified backend TLS is reported with the directive and its line. |
| 12 | Certificates referenced by no configuration are not collected. |
| 13 | Intermediates are identified and excluded from expiry lists by default. |
| 14 | A referenced certificate path that does not exist is reported as a finding, not an absence. |
| 15 | The renewal checklist is advisory only; nagipath performs no renewal action. |
| 16 | Per-Application certificate expiry is derived from that Application's Traces. |

---

## 6. Success signals

**A design partner finds an expiring or expired certificate they were not tracking.** In every fleet examined so far there is at least one, usually internal.

Supporting: certificates found versus certificates the customer's spreadsheet knew about (the gap is the pitch, and it is measurable in the first week); coverage-mismatch findings per install; how often the renewal checklist is exported, which is the strongest available signal for whether write support is what customers actually want next.

---

## 7. Risks

| Risk | Mitigation |
|---|---|
| A collector accidentally transfers a private key | Enforced in the single file-reading path, not per adapter. Reviewed as a security invariant, stated in ADR-0009. The one thing in this pillar that cannot go wrong. |
| `openssl` absent on older hosts | Fallback with in-memory key stripping, recorded as a degraded extraction method. |
| Customers already have certificate monitoring | Positioned on what theirs cannot do: internal certificates, blast radius, coverage mismatch, and the certificate-to-Application link. Expiry dates alone are not the product. |
| Renewal checklist read as an offer to renew | Labelled advisory in the payload and on screen. v1 writes nothing, and that constraint is the product's credibility. |
| Certificates in a vault or terminated at a device we do not manage | Out of scope, stated. nagipath reports certificates referenced by managed web-server configuration. |
