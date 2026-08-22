# Product: Access, Trust and Audit

**Supporting capability.** Backend: [user_license_audit.md](../backend/user_license_audit.md), [credential.md](../backend/credential.md). UI: [../frontend/settings.md](../frontend/settings.md).
**Milestone:** M0.

---

## 1. The problem

nagipath asks an enterprise for SSH access to its production web tier.

That is the largest ask in the product, and it is made in the first five minutes. Everything else — the Traces, the Drift reports, the certificate blast radius — is worth nothing if the security review says no. So the access and audit story is not a compliance checkbox bolted on at the end; it is the gate the whole product passes through, and it has to be designed first.

The security reviewer's questions are predictable and specific:

- What happens to our credentials? Where are they stored, and encrypted with what?
- If your database is stolen, what does the attacker get?
- Can this tool change anything on our servers?
- What exactly does it run, and can we restrict it?
- Who can do what inside it, and is that recorded?
- Does it phone home?

Each has to have a short, verifiable answer. "It's secure" fails. "Trust on first use, then it caches the host key" fails harder, because that reviewer knows what that means.

---

## 2. The answers

### 2.1 Read-only, structurally

v1 writes nothing to any managed host. Not "we don't intend to" — the SSH transport has no write capability, and every command is drawn from a **closed enum** in Go source. There is no code path that accepts a command string from a user, a database row, or an API request.

That enum is the injection boundary. An operator cannot ask nagipath to run something arbitrary, so a compromised nagipath account cannot use it as a remote execution service against the fleet. This is the answer that converts a hard security conversation into a short one.

The one exception is documented rather than hidden: a **Probe** sends an HTTP GET or HEAD to an Entry Point. It touches no host filesystem, is GET/HEAD-only enforced at the database level, never scheduled, always attributed to a named operator, self-identifying in its User-Agent, and fully audited.

### 2.2 Credentials: what a stolen database yields

Nothing usable.

Private keys and passphrases are AES-256-GCM encrypted with a Master Key that lives **outside** the database, in a `0600` file (or an environment variable). The AAD binds each ciphertext to its column and row ID, so a stolen ciphertext cannot be moved to another row and decrypted.

**The server refuses to start if the Master Key is missing or malformed.** It does not generate a new one. A tool that silently re-keys itself has silently made every stored credential unreadable while appearing healthy, and the operator finds out at the worst possible moment.

Private key fields are **write-only everywhere**: never returned by the API, never rendered in the UI, never included in a diagnostics bundle. **No role can read them** — it is not a permission that exists to be granted, so there is no policy mistake that can expose them.

Key-based and SSH-certificate authentication only. Password authentication is not supported, which removes a whole class of stored secret.

### 2.3 Host keys: no trust on first use

No command runs on a Node until an operator explicitly accepts its SSH host key. The fingerprint is shown, the operator approves it, and the key is stored. A changed key **fails the connection** and requires re-approval, with both fingerprints displayed side by side.

Trust-on-first-use would be one line of code and it is the line that makes the tool MITM-able on first contact, against exactly the hosts that matter most. A reviewer who spots TOFU stops reading.

### 2.4 No network scanning, ever

Nodes come from an operator-supplied host list or an imported Ansible inventory. There is no CIDR scan, no port sweep, no discovery-by-probing anywhere in the codebase.

Auto-discovery sounds like a feature and is, in an enterprise, a red flag: a tool that scans networks trips IDS alerts, gets its host blocked, and turns a security review into an incident. Requiring an explicit host list is the more useful behaviour as well as the safer one.

### 2.5 A narrow, copy-pasteable sudoers grant

Some inspection commands need privilege. The grant is a specific `Cmnd_Alias` covering only read-only inspection and reads of configuration and access-log paths — nothing that can mutate. It is provided as a snippet the customer can paste and read in under a minute.

Where the grant is absent, collection **degrades and says which command was refused**, rather than failing opaquely or silently omitting data.

### 2.6 No phoning home

Licences are ed25519-signed tokens verified offline against a public key compiled into the binary. No telemetry, no update check, no callback. Air-gapped installation is a first-class target.

Nothing leaves the customer's network unless the operator exports it.

---

## 3. Who can do what

Two roles in v1, deliberately.

| | `admin` | `viewer` |
|---|---|---|
| View everything derived | ✅ | ✅ |
| Create Applications and Entry Points | ✅ | ✅ |
| Collect, Probe, manage Nodes and Credentials, approve host keys, manage users and settings | ✅ | ❌ |
| Read a private key or decrypted secret | ❌ | ❌ |

`viewer` can create Applications on purpose. An Application is a label over derived data and creating one is harmless, while making it admin-only means a developer files a ticket to ask a question about their own service — which is precisely the friction that kills adoption of a tool whose value depends on developers using it.

Every check goes through one function with a scope parameter that is always global in v1. The parameter exists unused because v1.1 adds Application-scoped viewers, and retrofitting a scope argument into forty call sites afterwards is how authorization bugs get introduced.

**Local accounts only in v1.** Every enterprise will ask for SSO; the answer is v1.1. The reason is specific rather than a general appeal to simplicity: each customer's directory is configured differently and its failures — group DNs, referrals, nested groups, certificate trust — land on us and cannot be reproduced locally. One part-time engineer cannot carry that alongside three config parsers. What v1 does is make it cheap to add: `auth_source` exists from the first migration.

---

## 4. Audit

Every consequential action is recorded append-only: logins and failures, user and token lifecycle, Node and Credential changes, **host key approvals**, **every Probe**, Collection triggers, retention changes, licence installation, retirement and purge.

Four properties earn it the name:

- **Denials are recorded.** A successful admin action is expected; a stream of denials is a signal. `outcome` distinguishes `success`, `denied` and `failed`.
- **Exempt from retention.** Snapshots are pruned; audit rows are not. A trail with a 90-day horizon fails the one review it exists for.
- **Readable after someone leaves.** `actor_label` is denormalised, so a deleted user's actions still read `"aallen"` rather than `"user 7"`. Someone leaving the company is exactly when the log gets read.
- **Never contains secrets.** Detail payloads pass the same allowlist as API responses.

Reads are not audited. An audit log that records page views becomes a log nobody reads.

Both roles can see the audit log. A `viewer` who can see that a Probe was run, by whom, and against what is a feature — transparency about what the tool did to their fleet is what earns it continued access.

---

## 5. Journeys

### 5.1 The security review

The reviewer gets a one-page answer: read-only with a closed command enum; AES-256-GCM credentials with the key outside the database; mandatory host key approval; no network scanning; a pasteable sudoers grant; offline licensing; append-only audit including every Probe. The review takes an hour instead of a quarter.

### 5.2 Onboarding

First-run wizard creates one admin. No default password, no bypass. Master Key is generated to a `0600` file with its path displayed and a warning that losing it makes stored credentials unrecoverable.

### 5.3 The changed host key

A Node fails after a rebuild. Both fingerprints are shown side by side with the approval date of the old one. The operator confirms the rebuild and re-approves — an event that is itself audited. The failure was correct behaviour and the recovery took thirty seconds.

### 5.4 Offboarding

Delete the user. Sessions and tokens cascade; access ends immediately. Their audit history remains intact and readable.

### 5.5 "Did this tool touch our servers?"

Filter the audit log by Node. Every Collection, every host key approval, every Probe, with actor and timestamp. The answer is a list, not an assurance.

---

## 6. Acceptance criteria

| # | Criterion |
|---|---|
| 1 | Every remote command comes from a closed enum in source. No user-supplied command string reaches the SSH transport. |
| 2 | No code path writes to a managed host. |
| 3 | Private keys and passphrases are AES-256-GCM encrypted with AAD binding them to column and row. |
| 4 | The Master Key is stored outside the database; the server refuses to start if it is missing or malformed, and never generates a replacement. |
| 5 | Private key material is never returned by the API, rendered in the UI, or included in a diagnostics bundle, for any role. |
| 6 | Key and certificate authentication only; no password authentication. |
| 7 | No command runs on a Node before its host key is explicitly approved. |
| 8 | A changed host key fails the connection and requires re-approval, showing both fingerprints. |
| 9 | No network scanning capability exists. Nodes come only from operator input or imported inventory. |
| 10 | The sudoers grant is documented, minimal, and covers no mutating command. |
| 11 | Missing privilege degrades collection with the refused command named. |
| 12 | Probes are GET/HEAD only (database-enforced), attributed, never scheduled, rate-limited, self-identifying and audited. |
| 13 | Licence verification is offline; the binary makes no outbound network call of its own. |
| 14 | Every authorization decision passes through one function; denials return `403` and write an audit event. |
| 15 | Audit is append-only, exempt from retention, and readable after the actor is deleted. |
| 16 | Audit payloads never contain secrets. |
| 17 | Passwords are argon2id-hashed; sessions and API tokens are stored hashed. |
| 18 | The last admin cannot be deleted or demoted. |

---

## 7. Success signals

**Security review duration, and the review's blocking objections.** If reviews take a week and raise no blockers, this pillar has done its job. If a blocker recurs across customers, it is the next thing to build.

Supporting: how often the audit log is actually queried (a log nobody opens is a log nobody trusts); host key re-approval events, which indicate the mechanism is being exercised rather than worked around; and whether any customer asks to disable a guardrail — each such request is a real design signal.

---

## 8. Risks

| Risk | Mitigation |
|---|---|
| Master Key loss makes every credential unrecoverable | Stated plainly at generation, in the docs, and in the settings UI. Backup guidance provided. Refusing to start is the correct behaviour, not a bug to be softened. |
| A future contributor adds a write path or a command string parameter | Closed enum plus ADR-0002. The enum makes the wrong change visible in review rather than plausible. |
| SSO absence blocks an enterprise deal | Accepted v1 limitation, stated up front rather than discovered. `auth_source` makes it a v1.1 addition. |
| A single compromised admin account | Bounded by design: read-only fleet access, no credential read-back, everything audited. The blast radius is inspection, not execution. |
| Customers want per-Application access for developers | The main v1.1 item, and the scope parameter is already threaded through. |
| Two roles are too coarse for a large organisation | Real, and the reason Application scoping is prioritised over SSO. |
