# Certificates

**Assumes:** [design_system.md](design_system.md). **Backend:** [certificate.md](../backend/certificate.md).
**Product:** [../product/certificate_exposure.md](../product/certificate_exposure.md). **Milestone:** M6.

---

## 1. What these screens are

Not a certificate expiry dashboard. A dozen tools do that, and the customer probably owns one.

These screens answer the question the customer's existing monitoring cannot: **what breaks when this expires, and where exactly do I have to change it?** That requires knowing every place one certificate is deployed, which requires fingerprint identity and a fleet-wide configuration parse — and it is why this pillar is worth building at all.

Routes: `/certificates`, `/certificates/{id}`, `/certificates/findings`, `/certificates/expiring`.

---

## 2. `/certificates` — List

```
┌────────────────────────────────────────────────────────────────────────────┐
│  Certificates                                          collected 2h ago    │
│                                                                            │
│  expires [ any ▾ ]  issuer [ all ▾ ]  cluster [ all ▾ ]  ☐ include CAs     │
│  ☐ findings only                        [ search CN or SAN            ]    │
│                                                                            │
│  ⚠ 1 certificate expires within 30 days · 1 expired · 3 with findings      │
├────────────────────────────────────────────────────────────────────────────┤
│  24d  payments.corp.example                                          ⚠     │
│       + 1 SAN · RSA 2048 · Corp Issuing CA 2                                │
│       12 bindings · 6 nodes · 2 applications · combined PEM                 │
│       ⚠ served on a site whose hostname it does not cover            [ → ] │
│                                                                            │
│  exp   internal-tools.corp.example                                    ✗    │
│  42d   expired 42 days ago · self-signed · 2 bindings · 1 node        [ → ] │
│  ago                                                                       │
│                                                                            │
│  118d *.corp.example                                                       │
│       RSA 4096 · Corp Issuing CA 2 · 31 bindings · 18 nodes · 6 apps  [ → ] │
│                                                                            │
│  312d api-gateway.corp.example                                             │
│       ECDSA P-256 · 4 bindings · 2 nodes · 1 application             [ → ] │
└────────────────────────────────────────────────────────────────────────────┘
```

Sorted by soonest expiry by default, because that is the reason anyone opens this page.

**Days remaining is the leading column**, not the expiry date. `24d` is instantly comparable; `2026-09-14` requires arithmetic, and the arithmetic is where people misjudge urgency.

**Binding, node and application counts are on the list row**, not hidden in detail. They are what distinguishes a certificate that needs one change from one that needs twelve, and that distinction changes who has to do the work — so it belongs where the operator triages.

**`include CAs` is unchecked by default.** An intermediate expiring in eight years is not a finding, and mixing intermediates into an expiry list trains people to skim it. The checkbox exists because occasionally the intermediate is the question.

**Finding chips** appear inline on the row. A coverage mismatch is often more urgent than an expiry ninety days out, and burying it in detail would mean nobody sees it.

---

## 3. `/certificates/{id}` — Detail

Four blocks, in order of what the operator needs.

### 3.1 Identity

Fingerprint (monospace, copyable), subject CN, all SANs, issuer, validity window, days remaining, key algorithm and size, signature algorithm, self-signed flag, chain depth, first seen, last seen.

`first seen` is quietly valuable: "this certificate has been in your fleet for fourteen months" is an answer nobody else has, and it comes from the Certificate row surviving Snapshot retention.

### 3.2 Impact — the headline

```
┌──────────────────────────────────────────────────────────────────────┐
│  Impact                                                              │
│                                                                      │
│  Replacing this certificate requires updating 12 bindings across      │
│  6 nodes. 4 of those files are combined PEMs that also contain        │
│  private keys.                                                       │
│                                                                      │
│  6 instances · 9 sites · 2 applications                              │
│    payments-prod    payments-platform@corp.example                    │
│    api-gateway      platform-eng@corp.example                         │
└──────────────────────────────────────────────────────────────────────┘
```

A generated sentence, not a stat grid. It is the thing the operator pastes into a change ticket, and prose pastes better than a table.

The combined-PEM count is called out because it changes the nature of the work: those files also contain private keys, which changes who is allowed to touch them and under what change control.

### 3.3 Bindings

Table: Instance · Node · Vendor · Site · Listener · file path · combined PEM · chain depth · coverage · provenance.

**Coverage** is per-Binding, because the same certificate can be correct on one Site and wrong on another:

| Chip | Meaning |
|---|---|
| `covered` | CN or a SAN matches every name this Site serves |
| `wildcard only` ⚠ | Matched only by a wildcard — works, but the wildcard's blast radius is worth knowing |
| `not covered` ✗ | The Site serves a name this certificate does not cover. Clients get a name mismatch. |

`not covered` rows expand to name the exact Site hostnames that are uncovered. That is the finding, and a chip alone would not be actionable.

### 3.4 Renewal checklist

```
┌──────────────────────────────────────────────────────────────────────┐
│  Renewal checklist                                    [ copy ] [ ↓ ] │
│                                                                      │
│  ⓘ This is a list of what to change. nagipath does not perform        │
│    renewals and does not write to your servers.                      │
│                                                                      │
│  lb01.corp.example                                                   │
│    /etc/haproxy/certs/payments.pem     combined PEM — contains a key  │
│    1 instance · reload: haproxy reload required after replacement     │
│                                                                      │
│  web02.corp.example                                                  │
│    /etc/nginx/ssl/payments.crt                                       │
│    /etc/nginx/ssl/payments.key          not read by nagipath          │
│    2 instances · reload: nginx -s reload                             │
└──────────────────────────────────────────────────────────────────────┘
```

Grouped by Node because that is the unit of work. Exports as Markdown for a ticket.

Two notes matter. **The advisory disclaimer** — v1 writes nothing, and the checklist is the honest maximum of useful help within that constraint. And **`not read by nagipath`** on the key path, which reassures the reader mid-task at exactly the moment they are thinking about key files.

The export count is also our best signal for whether write support is what customers actually want next.

---

## 4. `/certificates/findings`

Grouped by kind, so the pillar has one actionable landing page.

| Group | Content |
|---|---|
| Coverage mismatches | `not covered` and `wildcard only`, with the specific hostnames |
| Missing files | A referenced certificate path that does not exist — a Site configured for TLS with no certificate, which is a serious finding rather than an absence |
| Expired but still bound | Frequently the reason someone installed the tool |
| Weak configuration | Keys under 2048 bits, SHA-1 signatures, validity over 398 days, self-signed on public-looking names |
| Unverified backend TLS | `proxy_ssl_verify off`, `SSLProxyVerify none`, HAProxy `verify none`, with directive and line |

Every finding is an **observation with a reason**, never a score or a grade. A letter grade invites arguing with the grade instead of fixing the certificate — and a self-signed certificate on an internal Site is a legitimate deliberate choice, so calling it a violation loses the trust of the person who made it.

Each row can be dismissed with a reason, admin-only, audited, and dismissals stay visible in a count. Same discipline as Drift ignore rules, same reason.

---

## 5. Where certificates appear elsewhere

- **Fleet overview** — one "needs attention" row for anything under 30 days.
- **Instance detail** — a Certificates tab with that Instance's Bindings.
- **Application footprint** — soonest expiry among the certificates that Application depends on, derived from its Traces. The Application owner's view, for someone who does not know which Instances serve them.
- **Trace Hop cards** — a TLS Listener shows its certificate with days remaining. Discovering an expiring certificate while debugging a request path is one of the more pleasant surprises the product delivers.

---

## 6. What is never shown

No screen, panel, export, tooltip or diagnostics bundle displays private key material. There is no permission that grants it, no admin override, no debug view.

The Credential form states this at the point of anxiety ([onboarding.md](onboarding.md)); the certificate screens state it in the renewal checklist. Both are deliberate: the guarantee should be visible where the reader is thinking about keys, not only on a security page they will never open.

---

## 7. Loading, error and empty states

| State | Treatment |
|---|---|
| No certificates collected | `No certificates found yet.` Then: *certificates are discovered from configuration during collection; only certificates referenced by a directive are collected.* Distinguishes "not collected yet" from "you have none". |
| Fleet genuinely serves no TLS | Same copy. The explanation covers both, correctly. |
| Filtered to nothing | `No certificates match these filters` + `Clear filters`. |
| Metadata extracted without `openssl` | `metadata extracted without openssl` chip; hover explains the fallback and that key blocks were stripped in memory before parsing. Honest about the method. |
| Referenced path missing on the host | Red `file not found` row with the provenance link to the directive that references it. A finding, not an omission. |
| Certificate expires while the Instance is quarantined | Still listed, with `⚠ last collected 14 days ago — this may have been replaced`. Stale certificate data presented as current would be actively misleading. |
| Certificate on a degraded Snapshot | Amber chip: `collected with gaps — bindings may be incomplete`. |
| SNI: several certificates on one Listener | All listed against that Listener, each with its Site. |
| Certificate rotated since last Collection | Old and new both present, old showing `last seen 2026-08-14`. A timeline, not an overwrite. |
| Wildcard covering 31 Sites | Rendered with a count and an expander. Thirty-one rows by default is not triage. |
| `viewer` clicks dismiss | Disabled with the role hint. |

---

## 8. Acceptance criteria

| # | Criterion |
|---|---|
| 1 | No screen, export or bundle ever displays private key material. |
| 2 | One certificate deployed many times is one row with many Bindings. |
| 3 | Days remaining is the leading column; the absolute date is available. |
| 4 | Binding, node and application counts appear on the list row. |
| 5 | Intermediates are excluded by default with an opt-in toggle. |
| 6 | Impact is a generated prose sentence naming bindings, nodes and combined-PEM count. |
| 7 | Combined PEM files are flagged at both Binding and Certificate level. |
| 8 | Coverage is per-Binding and expands to name uncovered hostnames. |
| 9 | Findings are observations with reasons; no score, grade or percentage appears. |
| 10 | The renewal checklist is grouped by Node, includes each Vendor's reload requirement, and is exportable. |
| 11 | The checklist states that nagipath performs no renewal and writes nothing. |
| 12 | Key file paths in the checklist are marked as not read by nagipath. |
| 13 | A referenced certificate path that does not exist is a red finding with a provenance link. |
| 14 | Non-`openssl` extraction is chipped and explained. |
| 15 | Certificate data from a quarantined or stale Instance carries a staleness warning. |
| 16 | Unverified backend TLS is reported with directive and line. |
| 17 | Application-scoped certificate expiry is available and derived from Traces. |
| 18 | Empty state distinguishes "not yet collected" from "none referenced by configuration". |
| 19 | Every certificate view is deep-linkable. |
