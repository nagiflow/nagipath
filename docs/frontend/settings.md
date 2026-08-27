# Settings

**Assumes:** [design_system.md](design_system.md). **Backend:** [user_license_audit.md](../backend/user_license_audit.md), [credential.md](../backend/credential.md), [collection.md](../backend/collection.md).
**Product:** [../product/access_and_audit.md](../product/access_and_audit.md), [../product/collection_scheduling_and_retention.md](../product/collection_scheduling_and_retention.md). **Milestone:** M0.

---

## 1. Sections

| Route | Section | Role |
|---|---|---|
| `/settings/credentials` | Credentials | admin |
| `/settings/hostkeys` | Host keys | admin |
| `/settings/collection-defaults` | Collection defaults | admin |
| `/collections` | Collection jobs | admin |
| `/settings/retention` | Retention and storage | admin |
| `/settings/users` | Users | admin |
| `/settings/api-keys` | API tokens | admin (own tokens) |
| `/settings/audit` | Audit log | **both roles** |
| `/settings/license` | Licence | admin to change, both to view |
| `/settings/system` | System and diagnostics | admin |
| `/settings/setup` | Setup checklist | admin |

`viewer` sees only Audit, Licence (read-only) and their own profile. Denied sections are **absent from the nav**, not present-and-broken — a nav item that always 403s is a daily reminder of a permission the user will never have.

---

## 2. Credentials

List: name · type · username · scope · used by N nodes · created.

**Nothing in this list hints at key content.** No prefix, no length, no truncated key. `ed25519 · SHA256:9x4k…` — the public fingerprint, which is not a secret.

**Add / Edit** — the form from [onboarding.md §5](onboarding.md), with the encryption and write-only guarantees stated inline beneath it, permanently, not in a collapsible. On edit the key field renders empty with the placeholder `unchanged`; submitting it empty leaves the stored key alone. There is no version of this screen that displays a key.

**Delete** — `422 credential_in_use` when Nodes depend on it, naming them with links. Not a cascade: deleting a Credential that 41 Nodes use would silently break the whole fleet's collection.

**Rotation blast radius** — before saving a change to an in-use Credential:

```
  This credential is used by 41 nodes. If the new key is not yet installed
  on those hosts, their next collection will fail with an authentication
  error. Existing snapshots are unaffected.

  [ Cancel ]                                        [ Save credential ]
```

Stating the consequence, and stating what is *not* affected, is what lets someone proceed confidently.

---

## 3. Host keys

Three groups: **awaiting approval**, **approved**, **changed** (danger, top).

Approved rows: hostname · key type · fingerprint · approved at · approved by · `Revoke`.

Changed rows use the side-by-side treatment with typed confirmation from [onboarding.md §7](onboarding.md). Both fingerprints, the prior approval date and approver, and the sentence that this is what a rebuild looks like *and* what a man-in-the-middle looks like.

`Revoke` returns the Node to awaiting-approval; its next connection fails until re-approved. Confirm names that consequence.

**There is no "trust all future host keys" toggle**, and the section states so in one line. A reviewer looks for that toggle, and finding an explicit statement that it does not exist is worth more than its silent absence.

---

## 4. Collection schedule

```
  Default schedule       [ daily ▾ ]  at [ 02:00 ]  jitter [ ±30 min ]
  Concurrent SSH workers [ 8 ]                        1 per node, always
  Command timeout        [ 30 ] seconds
  Quarantine after       [ 10 ] consecutive failures

  Per-node overrides                                    3 nodes  [ manage ]
```

Each field carries a one-line consequence, because these numbers look arbitrary until you know what they trade:

- Jitter: *prevents every node collecting in the same second.*
- Workers: *higher finishes sooner and puts more simultaneous load on your fleet.*
- Per-node concurrency: *fixed at 1. A node is never collected twice at once.*
- Quarantine: *stops scheduled collection after repeated failure. Manual collection always works and clears it.*

Below: **quarantined nodes**, listed with failure reason, count and last success, each with `Collect now`. This is where an operator lands when the dashboard says something is quarantined, so the fix is on the same screen as the setting.

---

## 5. Retention and storage

```
  Snapshots per instance   [ 30 ]        the current snapshot is never pruned
  Maximum age              [ 90 ] days
                                         whichever retains more

  Storage
    database        412 MB
    stored files    1,284,301 rows
    unique blobs    18,442        deduplication 14.2×
    audit events    44,120        exempt from retention

  [ Preview retention change ]   [ Run blob garbage collection ]
```

**Preview before apply** is the important control:

```
  Changing to 14 snapshots / 30 days would delete
    1,102 snapshots · 892,004 file rows · 11,208 blobs
    reclaiming approximately 268 MB

  ⚠ 3 retained traces become unreproducible. They keep their shape — hops,
    instances and paths — but lose per-hop rule detail. Retrace recomputes
    them against current configuration. Probe evidence is unaffected.

  [ Cancel ]                                    [ Apply retention change ]
```

Nobody should have to guess the cost of a retention setting. The Trace warning states the settled behaviour rather than hedging: **retention is never blocked by a Trace and nothing is pinned** (ADR-0015), because storage growth an operator cannot predict or reclaim is worse than a Trace that admits it can no longer show its evidence. Naming the recovery (`Retrace`) and the exemption (Probe evidence) in the same breath is what keeps the warning from reading as data loss.

The deduplication ratio is displayed because it is the number that justifies daily collection with 90-day retention on a small VM, and because it is genuinely satisfying to look at.

---

## 6. Users

List: username · display name · role · auth source · last login · created.

`Add user`, `Edit`, `Reset password`, `Delete`.

- **Reset password** sets `must_change_password`; an admin never chooses a lasting password for someone else.
- **Delete** — `Deleting jsmith ends their sessions and revokes their API tokens immediately. Their audit history is kept.` The second sentence is what makes offboarding comfortable.
- **`422 last_admin`** on deleting or demoting the only admin, inline: `This is the only administrator. Promote another user first.` Self-deletion refused identically.

A permanent note: **local accounts only in v1; LDAP and SAML are planned for v1.1**, with `auth_source` already in the schema. Stating the roadmap here is better than a customer discovering the gap during a security review.

The role matrix is shown inline, including the row **nobody can read credential secrets**. That row is the point of showing the matrix at all.

---

## 7. API tokens

Own tokens only, even for admins. An admin who could mint tokens for other users could impersonate them, and no feature needs that.

Create: name, expiry (default **90 days**, not "never" — a default of never is how a token outlives the person who made it).

The plaintext appears once:

```
  npat_7f3k9x2mQ8vB4nL6pR1sT5wY0zA3cE

  [ copy ]   ⚠ This is the only time this value is shown. It cannot be
             retrieved again. Store it in your secret manager now.
```

List shows name, created, last used, expires — never a hash, never a prefix long enough to be useful.

`last used` is what makes stale tokens findable, and the section sorts by it so the oldest-unused sit at the top.

---

## 8. Audit log

Visible to **both roles**, deliberately. A `viewer` who can see that a Probe was run, by whom, and against what is a feature: transparency about what the tool did to their fleet is what earns it continued access.

Filters: actor, action, object type, object, outcome, date range, free text.

```
  09:31:02  aallen   probe.create        entry_point 11    success
            GET https://payments.corp.example/api/v2/charge
            from nagipath01.corp.example · probe 4410
  09:12:44  jsmith   probe.create        entry_point 11    denied
            role viewer may not create probes
  16:02:10  aallen   host_key.approve    node 44           success
            ssh-ed25519  SHA256:9x4k…
```

**Denied rows are styled distinctly.** A successful admin action is expected; a run of denials is a signal, and the log should draw the eye to it.

Deleted users show their retained `actor_label`, so history stays readable after someone leaves — which is exactly when it gets read.

`Export CSV` is available and is itself audited. `Export and truncate` is admin-only, requires typed confirmation, and is audited — bounded growth achieved by a recorded decision rather than silent deletion.

A permanent note: **audit events are exempt from retention. Reads are not audited.** Both answer questions a reviewer will otherwise ask.

---

## 9. Licence

```
  Status        active
  Customer      Corp Ltd
  Tier          enterprise
  Nodes         47 of 200
  Expires       2027-01-01     133 days

  ⓘ Enforcement is soft. Collections and traces are never blocked, even if
    the licence expires or the node limit is exceeded.

  [ Install a new licence ]
```

That note appears **in every licence state, including over-limit and expired**. Nobody debugging a licence problem should have to wonder whether their fleet data collection has stopped.

| State | Treatment |
|---|---|
| No licence | `Community · 10 nodes · full functionality`. Not styled as degraded — the free tier is real. |
| Over limit | Warning banner naming count and limit. New Nodes still create. |
| Expired | Warning banner naming the date. Everything continues. |
| Invalid signature | `invalid` with the reason, community limits applied. A tampered licence gets community limits, not a lockout. |

Install: paste the token. `422 invalid_signature` before anything is written. Verification is offline against a public key compiled into the binary — stated on screen, because "does it phone home" is a question worth answering where it is asked.

---

## 10. System and diagnostics

Read-only: version, build commit, Go version, database path and size, Master Key path (**path only, never contents**), listen address, TLS status, demo mode, uptime, licence status, scheduler state, worker pool utilisation, migrations applied.

**Master Key block:**

```
  Master key   /var/lib/nagipath/master.key   present, mode 0600

  ⚠ Back this file up. Credentials in the database cannot be decrypted
    without it, and nagipath will refuse to start if it is missing — it
    will never generate a replacement.
```

The warning is repeated here because the person reading Settings six months later is not the person who read the terminal output on day one.

**Diagnostics bundle** — version, configuration with secrets redacted, recent logs, migration state, collection statistics, error counts. Explicitly excludes: credentials, Master Key, session and token values, certificate material, Probe tokens, configuration file contents.

The exclusion list is shown before the download. A support bundle is the classic accidental-secret-exfiltration path, and someone about to email it to us deserves to see exactly what they are sending.

---

## 11. Loading, error and empty states

| State | Treatment |
|---|---|
| `viewer` reaches an admin URL directly | `403` page: `This section requires the admin role.` No retry button — retrying will not help. |
| Demo mode | Credentials and Users read-only with `disabled in demo mode`; Probes disabled; retention and schedule read-only. |
| Delete an in-use Credential | `422` inline naming the dependent Nodes with links. |
| Rotate an in-use Credential | Blast-radius confirmation naming the Node count. |
| Retention change that would delete everything | Blocked: the current Snapshot per Instance is never prunable. Stated in the preview. |
| Blob GC already running | Button disabled, `garbage collection in progress`. |
| Concurrent settings edits | Last write wins; a toast notes another admin changed this recently, with the actor. |
| Token creation while over rate limit | `429` with the retry window. |
| Audit log with 500,000 rows | Cursor pagination; date filters default to the last 7 days with the total count shown. |
| Audit export of a large range | Streamed with progress; never buffered in memory. |
| Licence expired | Banner everywhere; all settings remain editable. |
| Master Key missing | Not a UI state. The process refuses to start. |

---

## 12. Acceptance criteria

| # | Criterion |
|---|---|
| 1 | No screen displays private key material, a session value, or a token hash. |
| 2 | Credential edit shows an empty key field; empty submission preserves the stored key. |
| 3 | Credential deletion is blocked while Nodes depend on it, naming them. |
| 4 | Credential rotation shows the blast radius and states what is unaffected. |
| 5 | No trust-all-host-keys option exists, and the section says so. |
| 6 | Changed host keys require typed confirmation and show both fingerprints with the prior approval. |
| 7 | Every schedule field states its consequence in one line. |
| 8 | Quarantined Nodes are listed on the schedule page with `Collect now`. |
| 9 | Retention changes are previewed with counts and reclaimable space before applying. |
| 10 | The retention preview names how many retained Traces become unreproducible, and states that Retrace recovers them and Probe evidence is unaffected. |
| 11 | Retention is never blocked or deferred by a Trace, and no Snapshot is pinned on a Trace's behalf. |
| 12 | The current Snapshot per Instance can never be pruned. |
| 13 | Deduplication ratio and database size are displayed. |
| 14 | The last admin cannot be deleted or demoted. |
| 15 | Admin password reset forces a change rather than setting a lasting password. |
| 16 | The role matrix is shown inline, including that no role can read credential secrets. |
| 17 | API tokens are own-only; plaintext appears exactly once with an explicit warning. |
| 18 | Token expiry defaults to 90 days, not never. |
| 19 | The audit log is visible to both roles, with denied events styled distinctly. |
| 20 | Deleted users' audit rows remain readable via the retained label. |
| 21 | Audit exports and truncations are themselves audited. |
| 22 | The licence page states soft enforcement in every state, including expired and over-limit. |
| 23 | Licence verification is offline and stated as such. |
| 24 | The Master Key path is shown; contents never are; the backup warning is present. |
| 25 | The diagnostics bundle's exclusion list is displayed before download. |
| 26 | Denied sections are absent from the nav rather than present and failing. |
