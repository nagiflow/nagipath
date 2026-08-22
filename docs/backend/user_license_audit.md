# User, Session, API Token, Audit Event, License

**Schema:** [schema.md §13](schema.md#13-access-and-audit). **Conventions:** [api_conventions.md](api_conventions.md).
**Decisions:** ADR-0013 (local accounts, two roles), ADR-0014 (offline license, soft enforcement).
**Product spec:** [../product/access_and_audit.md](../product/access_and_audit.md).

---

## 1. Overview

Five entities that share one concern: **who is allowed to do what, and what did they actually do.**

- **App User** — a local account. Username, argon2id password hash, role.
- **User Session** — a browser session, cookie-backed.
- **API Token** — a bearer credential for `nagipath ctl` and CI, hashed at rest.
- **Audit Event** — an append-only record of a consequential action.
- **License State** — the single row recording the installed license and its verification result.

These are grouped in one document because they are one story, and because splitting them would hide the thing that matters most: **every one of them exists to make the tool safe to install in an enterprise that has just been asked to hand it SSH access to its production web tier.**

---

## 2. Authentication

### 2.1 Local accounts only in v1

No LDAP, no SAML, no OIDC (ADR-0013). Every enterprise buyer will ask for SSO, and the answer is v1.1.

The reasoning is specific rather than a general "keep it simple": SSO integration is a support burden out of proportion to its build cost. Each customer's directory is configured differently, and the failures — group DN mismatches, referral chasing, certificate trust, nested groups — land on us and cannot be reproduced locally. One part-time engineer cannot carry that in v1 alongside three config parsers.

What v1 does instead is make SSO cheap to add later: `app_user.auth_source` exists from the first migration with a value of `'local'`, and every authorization decision goes through one function. Adding a provider means a new `auth_source` value and a login handler, not a refactor.

### 2.2 Password handling

argon2id via `golang.org/x/crypto/argon2`, per-user salt, parameters stored alongside the hash so they can be raised later without invalidating existing hashes. Constant-time comparison. Failed attempts rate-limited per username **and** per source IP — per-username alone is a trivial lockout DoS against a known admin account.

The first-run flow creates one admin interactively and **requires** a password change on first login if one was seeded from the environment. There is no default password, and the server will not start with an empty user table plus a bypass — the first-run wizard is a real gate, not a suggestion.

### 2.3 Sessions

Opaque 256-bit random token, `Secure` (when TLS is on), `HttpOnly`, `SameSite=Lax`. Stored **hashed**, so a stolen database yields no live sessions. Absolute lifetime 12 hours, idle timeout 2 hours, both configurable. Logout deletes the row rather than relying on cookie expiry.

Deleting a User cascades their Sessions and Tokens. Access ends immediately, which is the whole point of an offboarding action.

### 2.4 API tokens

For `nagipath ctl` and CI. `Authorization: Bearer <token>`. `token_hash` only — the plaintext is shown **exactly once**, at creation, and cannot be recovered. A token that can be re-read from the UI is a password with extra steps.

`expires_at` is optional but the creation form defaults to 90 days, because a default of "never" is how a token outlives the person who made it. `last_used_at` is updated on use so unused tokens can be found and revoked; it is written lazily (at most once per minute per token) so a busy CI job does not turn every request into a writer-pool write.

---

## 3. Authorization

### 3.1 Two roles

| | `admin` | `viewer` |
|---|---|---|
| View fleet, Traces, Drift, Certificates | ✅ | ✅ |
| Create Applications and Entry Points | ✅ | ✅ |
| Trigger a Collection | ✅ | ❌ |
| Run a **Probe** | ✅ | ❌ |
| Manage Nodes and Credentials | ✅ | ❌ |
| Approve Host Keys | ✅ | ❌ |
| Manage Users and Tokens | ✅ | ❌ |
| Change retention and schedules | ✅ | ❌ |
| Read Credential secrets | ❌ | ❌ |

The last row is the important one. **No role can read a private key or a decrypted secret.** It is not a permission that exists to be granted, so it is not modelled as one — there is no API path that returns key material, and therefore no policy mistake that can expose it.

`viewer` can create Applications deliberately: an Application is a label over derived data and creating one is harmless, while making it admin-only would mean a developer has to file a ticket to ask a question about their own service. That friction is exactly what kills adoption of a tool whose value depends on developers using it.

### 3.2 One chokepoint

```go
func (a *Authz) Can(u *User, action Action, scope Scope) error
```

Every mutating handler and every sensitive read calls it. `Action` is a closed enum. `Scope` is `{Kind, ID}` and in v1 is always `ScopeGlobal` — it exists unused because v1.1 adds Application-scoped viewers, and retrofitting a scope parameter into forty call sites afterwards is how authorization bugs are introduced. The unused parameter is cheap; the retrofit is not.

A denied call produces `403` **and an `audit_event` with `outcome='denied'`**. Denials are the more interesting half of an audit log: a successful admin action is expected, a stream of denials is a signal.

---

## 4. Audit

### 4.1 What is audited

Every action with an effect outside a read: login success and failure, logout, user and token lifecycle, Node and Credential create/update/delete, **host key approval**, **every Probe**, Collection triggers, retention and schedule changes, licence installation, retire and purge, drift ignore-rule changes.

Explicitly **not** audited: page views and ordinary reads. An audit log that records reads becomes a log nobody reads.

### 4.2 Append-only, in practice as well as intent

- No `UPDATE` or `DELETE` query exists for `audit_event` in the generated `sqlc` layer. Nothing can quietly edit history through the normal path.
- **Exempt from retention.** Snapshots and Traces are pruned; audit rows are not. An audit trail with a 90-day horizon fails the one review it exists for.
- `actor_label` is **denormalised** alongside `actor_user_id`. When a User is deleted the FK goes null, and the row still reads `"aallen"` rather than `"user 7"`. An audit trail that becomes unreadable when someone leaves the company is worthless, and someone leaving is exactly when it gets read.
- `outcome` is `'success' | 'denied' | 'failed'`, distinguishing "not allowed" from "allowed but broke".
- `detail_json` holds action-specific context and is filtered through the same allowlist as API responses. **Audit rows never contain secrets** — no key material, no password, no probe token.

### 4.3 Retention of the audit log itself

Unbounded, with a caveat stated honestly: a busy install accumulates rows indefinitely, and the row is small (a few hundred bytes) so this is measured in tens of megabytes per year rather than gigabytes. Export-then-truncate is available as an explicit admin action that is **itself audited**, which is the correct shape — bounded growth achieved by a recorded decision, not by silent deletion.

---

## 5. License

### 5.1 Offline verification

An ed25519-signed token pasted into the UI or supplied via `NAGIPATH_LICENSE_FILE`. Payload: customer name, tier, node limit, issue and expiry dates. The public key is compiled into the binary. **No network call, ever** (ADR-0014).

Air-gapped installs are a primary target, not an edge case; a licence check that phones home fails in exactly the environments where a web-server inventory tool is most needed. And a self-managed on-prem binary cannot be technically prevented from running — anyone with root can patch the check out — so enforcement that inconveniences honest customers buys nothing.

### 5.2 Soft enforcement, with one absolute rule

Over the node limit or past expiry:

- A persistent, non-dismissable banner naming the specific overage.
- The number in `license show`.
- **Collections continue. Traces continue. Nothing stops.**

The absolute rule: **an expired licence can never stop a Collection or a Trace.** The failure mode being avoided is concrete — an incident at 3am, an operator opening nagipath to find where a request goes, and being met by a licence wall. That converts the product from an asset into the reason the incident took longer, and no revenue protection is worth it. Licensing is a commercial conversation, and it is had in a banner.

### 5.3 Absent and invalid licences

| State | Behaviour |
|---|---|
| No licence | Community mode. Node limit 10, full functionality, a footer notice. Not degraded — the free tier is real. |
| Signature invalid | `license_state.status='invalid'`, treated as no licence, banner names the reason. A tampered licence gets community limits, not a lockout. |
| Expired | `status='expired'`, everything continues, banner shows the date. |
| Over node limit | `status='over_limit'`, banner names the count and the limit. New Nodes still create. |
| Clock wrong on the nagipath host | Expiry compared against the host clock; a wildly wrong clock can show a false expiry. Accepted — the consequence is a wrong banner, which is why enforcement is soft. |

That last row is the payoff of soft enforcement: a design where clock skew produces a cosmetic error rather than an outage.

### 5.4 SaaS and self-managed are the same binary

The same artefact, licensed differently. Our hosted offering is this binary run by us with a licence issued to ourselves. No SaaS-only code paths, no build tags separating editions, which means the self-managed build is never the untested one.

---

## 6. API

### `POST /api/v1/auth/login` · `POST /api/v1/auth/logout`

```json
{ "username": "aallen", "password": "…" }
```

`200` sets the session cookie and returns the user. `401 invalid_credentials` — deliberately identical for a wrong username and a wrong password, so the endpoint is not a username oracle. `429 login_rate_limited`. `403 password_change_required` with a redirect target.

### `GET /api/v1/auth/me`

```json
{ "id": 1, "username": "aallen", "display_name": "Allen Ng", "role": "admin",
  "auth_source": "local", "must_change_password": false,
  "session_expires_at": "2026-08-21T21:30:00Z",
  "capabilities": ["collect", "probe", "manage_nodes", "manage_credentials",
                   "approve_host_keys", "manage_users", "manage_settings"] }
```

`capabilities` is derived from the role, not stored. The frontend uses it to hide controls the user cannot use, which is a usability affordance and never the enforcement — `authz.Can()` is the enforcement.

### `GET /api/v1/users` · `POST` · `PATCH /{id}` · `DELETE /{id}`

Admin only. `password_hash` is never in a response. `422 last_admin` on deleting or demoting the only admin — locking everyone out of their own installation is not a state the API will help reach. Self-deletion is refused with the same code.

### `POST /api/v1/users/{id}/password`

Self-service requires the current password. An admin resetting another user's password sets `must_change_password=1` rather than choosing a lasting password for them.

### `GET /api/v1/tokens` · `POST` · `DELETE /{id}`

```json
{ "name": "ci-inventory-export", "expires_at": "2026-11-19T00:00:00Z" }
```

```json
{ "id": 12, "name": "ci-inventory-export", "token": "npat_…",
  "note": "This value is shown once and cannot be retrieved again." }
```

Listing returns `name`, `created_at`, `last_used_at`, `expires_at` — never a hash, never a prefix long enough to be useful to an attacker.

### `GET /api/v1/audit`

Filters: `actor_user_id`, `action`, `object_type`, `object_id`, `outcome`, `since`, `until`, `q`.

```json
{
  "items": [
    { "id": 44120, "at": "2026-08-21T09:31:02Z",
      "actor_user_id": 1, "actor_label": "aallen",
      "source_ip": "10.0.5.9", "user_agent": "Mozilla/5.0 …",
      "action": "probe.create", "object_type": "entry_point", "object_id": 11,
      "outcome": "success",
      "detail_json": { "url": "https://payments.corp.example/api/v2/charge",
                       "method": "GET", "probe_id": 4410,
                       "origin_host": "nagipath01.corp.example" } },
    { "id": 44119, "at": "2026-08-21T09:12:44Z",
      "actor_user_id": 3, "actor_label": "jsmith",
      "action": "probe.create", "object_type": "entry_point", "object_id": 11,
      "outcome": "denied",
      "detail_json": { "reason": "role viewer may not create probes" } },
    { "id": 44098, "at": "2026-08-20T16:02:10Z",
      "actor_user_id": 1, "actor_label": "aallen",
      "action": "host_key.approve", "object_type": "node", "object_id": 44,
      "outcome": "success",
      "detail_json": { "key_type": "ssh-ed25519", "fingerprint_sha256": "SHA256:9x…" } }
  ],
  "next_cursor": "…"
}
```

Visible to both roles. A `viewer` who can see that a Probe was run and by whom is a feature — transparency about what the tool did to their fleet is precisely what earns it continued access.

### `GET /api/v1/audit/export?format=csv&since=…`

Streamed, audited as `audit.export`.

### `GET /api/v1/license` · `POST /api/v1/license`

```json
{ "status": "active", "customer": "Corp Ltd", "tier": "enterprise",
  "node_limit": 200, "node_count": 47,
  "issued_at": "2026-01-01T00:00:00Z", "expires_at": "2027-01-01T00:00:00Z",
  "days_remaining": 133, "banner": null,
  "enforcement": "soft — collections and traces are never blocked" }
```

Over limit:

```json
{ "status": "over_limit", "node_limit": 200, "node_count": 214,
  "banner": { "severity": "warning",
              "message": "214 Nodes exceed the licensed limit of 200. All functionality continues; contact sales to extend the licence." },
  "enforcement": "soft — collections and traces are never blocked" }
```

`POST` installs a licence, verifies the signature before writing, `422 invalid_signature` otherwise, and audits the install. The `enforcement` field is returned unconditionally, including on failures — nobody debugging a licence problem should have to wonder whether their fleet data collection has stopped.

---

## 7. Edge cases

| Case | Behaviour |
|---|---|
| First run, no users | First-run wizard creates one admin. No default password, no bypass. |
| Admin seeded from environment | `must_change_password=1`; login redirects to a change form. |
| Last admin deleted or demoted | `422 last_admin`. Self-deletion refused likewise. |
| User deleted while logged in | Sessions and Tokens cascade. Access ends immediately. |
| User deleted, audit rows remain | `actor_user_id` null, `actor_label` retains the username. History stays readable. |
| Password brute force | Rate-limited per username **and** per source IP. Every failure audited. |
| Session cookie replayed after logout | Row deleted; `401`. |
| Token used after expiry | `401 token_expired`, distinct from `token_invalid`, so a CI failure is diagnosable. |
| Token plaintext lost | Unrecoverable by design. Revoke and reissue. |
| Database stolen | No plaintext passwords, no live sessions, no usable tokens, no readable Credentials without the separate Master Key. |
| Licence file missing at start-up | Community mode, 10 Nodes. Starts normally — unlike a missing Master Key, which refuses. Different severities, different behaviours. |
| Licence expires mid-incident | Banner appears. Collections and Traces continue. The rule that cannot be broken. |
| Licence signed by an unknown key | `invalid`, community limits. |
| Clock skew on the nagipath host | May show a wrong expiry. Cosmetic, because enforcement is soft. |
| Audit table grows large | Exempt from retention by design. Export-then-truncate is an explicit admin action, itself audited. |
| Probe run by an admin who later leaves | Probe, evidence and audit row all survive with `actor_label` intact. |
