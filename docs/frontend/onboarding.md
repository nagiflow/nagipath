# Onboarding

**Assumes:** [design_system.md](design_system.md). **Backend:** [node.md](../backend/node.md), [credential.md](../backend/credential.md), [user_license_audit.md](../backend/user_license_audit.md).
**Milestone:** M0.

---

## 1. What this flow has to achieve

From `./nagipath server` to a fleet inventory with a working Trace, **in under thirty minutes**, without reading documentation.

That number is the product's distribution strategy. nagipath competes against "we'll build a script for that", and the only reliable way to win is to be useful before the operator's patience runs out. Every step here is measured against it.

The flow also has to survive its own hardest moment: asking for SSH credentials to production, in the first five minutes, from someone who has known this tool for four minutes. The design answer is to be conspicuously careful — show fingerprints, require explicit approval, state plainly what is stored and where — because visible caution earns more trust than a smooth flow does.

---

## 2. Step 0 — First run in the terminal

```
$ nagipath server
nagipath 1.0.0

No master key found. Generating one at /var/lib/nagipath/master.key (mode 0600).

  ⚠  Back this file up now. Credentials stored in the database cannot be
     decrypted without it. nagipath will refuse to start if it is missing
     and will never generate a replacement.

Database initialised at /var/lib/nagipath/nagipath.db (14 migrations applied).
Listening on http://0.0.0.0:8080

No users exist. Open the URL above to create the first administrator.
```

Three deliberate choices here.

**The Master Key warning is in the terminal, not only in the UI.** The person who runs the binary is the person who can back the file up, and they are looking at a terminal.

**The server starts with no users.** It does not create a default admin with a printed password, because a printed password ends up in a shell history and a chat log.

**Migrations are announced.** An operator who sees "14 migrations applied" knows the database is real.

---

## 3. Step 1 — Create the administrator

Single-screen form: username, display name, password, confirm.

- Password strength shown as a live requirement checklist, not a coloured meter. A meter is an opinion; a checklist is testable.
- Minimum 12 characters, no composition rules. Composition rules produce `Password1!` and nothing more.
- Submit creates the user, logs in, and redirects to setup.

**Errors** — Username taken (`409`, inline). Passwords do not match (client-side, inline). Too short (inline, with the specific requirement). Submitting the form twice: the button disables on submit, and the second request gets `409` handled as "already created, logging you in".

If someone else reaches this screen first over the network, it is gone — first-run creation is single-use, and the second visitor sees the login page. That race is real on a shared host and is resolved by the database, not by a check-then-act.

---

## 4. Step 2 — The setup checklist

The hub. Five items, each with a state, and the page is reachable from the nav until every item is done.

```
┌──────────────────────────────────────────────────────────────────────┐
│  Get started                                          4 steps left   │
├──────────────────────────────────────────────────────────────────────┤
│  ✓  Administrator account            aallen                          │
│  ○  Add a credential                 SSH key or certificate    [Add] │
│  ○  Add nodes                        Host list or Ansible   [Import] │
│  ○  Approve host keys                — pending nodes                 │
│  ○  Collect                          — nodes ready                   │
│                                                                      │
│  Optional                                                            │
│  ○  Install a licence                Community: 10 nodes     [Add]   │
│  ○  Define an application            Trace works without one  [Add]  │
└──────────────────────────────────────────────────────────────────────┘
```

Ordered by dependency, not by importance. Credential first because a Node without one cannot be checked, and discovering that after importing 200 hosts is a bad first experience.

The two optional items are marked optional and say why they can be skipped. A licence is not needed to start, and a Trace works on an ad-hoc URL with no Application defined — telling the operator that is what lets them reach the payoff fastest.

The checklist stays available afterwards under Settings, because the second and third Node batches arrive weeks later.

---

## 5. Step 3 — Add a credential

Modal or full page.

**Fields** — Name (required). Type: SSH key / SSH certificate. Username. Private key (textarea, `autocomplete="off"`, `spellcheck="false"`). Passphrase (optional). Scope: default for all Nodes / specific Cluster / specific Node.

**Below the form, permanently visible, not in a collapsible:**

> Private keys are encrypted with AES-256-GCM before being written to the database. The encryption key is held in `/var/lib/nagipath/master.key`, outside the database. **This key can never be read back** — not through the API, not in the UI, not by any user role, not in a diagnostics bundle.

That paragraph is load-bearing. The operator pasting a production private key into a browser form is doing the most uncomfortable thing this product asks of them, and this is the moment to answer the question they are already asking.

**Behaviour** — On save, the field is cleared and replaced with `Key stored · ed25519 · SHA256:9x4k…`. On edit, the field renders empty with the placeholder `unchanged`, and submitting empty leaves the stored key alone. Write-only means there is no version of this screen that displays a key.

**Errors** — `422 unparseable_key` naming the failure ("not a recognised PEM block"). `422 passphrase_required` when the key is encrypted and no passphrase was given — detected at parse time, not at first connection failure, because a validation error five minutes later on a different screen is a bad experience. `422 password_auth_unsupported` if a password is somehow submitted, stating that key and certificate authentication only is a design decision.

---

## 6. Step 4 — Add nodes

Two tabs.

### 6.1 Paste a host list

Textarea, one host per line, optional `host:port` and `# comment`. Live parse count beneath: `47 hosts, 2 duplicates ignored, 1 invalid (line 12)` with line 12 highlighted.

### 6.2 Import an Ansible inventory

File upload or paste. INI and YAML.

**Dry run is the default and the checkbox is checked.** Results table:

```
  Would create   41   web01–web41 …
  Would update    4   lb01 (port 22 → 2222), …
  Unchanged       2   app01, app02
  Skipped         1   localhost (loopback)
```

Each row expandable to the mapped fields. Select a stored credential when adding the
nodes; its username and authentication method are used for collection. An explicit
`user@host` entry still overrides the credential username for that host. `ansible_host`
→ address, `ansible_port` → port, `ansible_user` → per-host username.

Three reconciliation rules are stated on screen because they surprise people:

- **Groups do not become Clusters.** An Ansible group is a deployment convenience; a Cluster is a claim that members should be configured identically. Inferring one from the other produces confident wrong Clusters, and Drift built on a wrong Cluster is noise.
- **Manually created Nodes are never modified** by an import.
- **Hosts absent from the inventory are never deleted.** An import is additive; retiring a Node is a separate, deliberate action.

**Buttons** — `Run dry run` (primary) → results. Then `Apply` (primary, now enabled) and `Back to edit`. Applying navigates to the Node list with a toast: `41 nodes created, 4 updated.`

**Errors** — `422 unparseable_inventory` with the line number. `422 no_hosts_found` for a valid file with no hosts, which usually means a dynamic inventory script rather than a static file, and the message says so.

**Empty state, before anything is added** — states plainly: *nagipath never scans networks. Nodes come only from a host list you provide or an inventory you import.* First question of every security review, answered before it is asked.

---

## 7. Step 5 — Approve host keys

The screen where the design's caution is most visible, and the one to resist streamlining.

Adding a Node **does not connect to it.** The first connection happens on an explicit `Check`, which fetches the host key and stops.

```
┌──────────────────────────────────────────────────────────────────────┐
│  Host keys awaiting approval                          41 pending     │
│                                                                      │
│  [ Check all pending nodes ]                                         │
├──────────────────────────────────────────────────────────────────────┤
│  ☐  web01.corp.example    ssh-ed25519  SHA256:9x4kQ…  [Approve]      │
│  ☐  web02.corp.example    ssh-ed25519  SHA256:9x4kQ…  [Approve]      │
│  ☐  lb01.corp.example     ssh-ed25519  SHA256:7bTmZ…  [Approve]      │
│                                                                      │
│  38 nodes share the fingerprint SHA256:9x4kQ… (common on cloned      │
│  images). [ Select all 38 ]                                          │
│                                                                      │
│  [ Approve selected (0) ]                                            │
└──────────────────────────────────────────────────────────────────────┘
```

Bulk approval exists, and grouping by shared fingerprint is what makes it responsible rather than a rubber stamp: the operator approves *one fingerprint* that happens to appear on 38 hosts, which is a meaningful decision they can verify against their image build. Approving 38 unrelated fingerprints in one click would not be.

**There is no "trust all future keys" option, and there never will be.** A changed key must fail.

**The changed-key case** — Both fingerprints side by side, the approval date of the old one, and the actor who approved it:

```
  ⚠  Host key changed for web12.corp.example

     Previously  ssh-ed25519  SHA256:9x4kQ…   approved 2026-03-04 by aallen
     Now         ssh-ed25519  SHA256:2pLmW…

     This is expected after a rebuild or reinstall. It is also what a
     man-in-the-middle attack looks like. Verify out of band before
     approving.

     Type the node's hostname to confirm:  [__________]  [ Re-approve ]
```

Typed confirmation, because this is the one click in the product that can genuinely be a security incident. The event is audited with both fingerprints.

**Errors** — Connection refused, timeout, DNS failure and auth failure each get their specific message and a `Retry`. Auth failure additionally links to the Credential, because that is the actual fix nine times out of ten.

---

## 8. Step 6 — First collection

`Collect all` on the ready Nodes. Live progress list, one row per Node, polled every 3 seconds.

```
  ✓  web01   nginx 1.24.0 · 1 instance  · 62 files · 4.1s
  ✓  lb01    haproxy 2.8.5 · 1 instance · 3 files  · 1.2s
  ⟳  web03   collecting configuration…
  ⚠  web07   degraded — openssl not found; certificate metadata skipped
  ✗  web12   failed — sudo: a password is required for 'nginx -T'
  ○  web13   queued
```

Individual failures are shown as they happen, not summarised at the end, so the operator can start fixing the first problem while the rest run.

Failure rows expand to the exact command that failed, its stderr, and the fix — for the sudo case, the copy-pasteable `Cmnd_Alias NAGIPATH_INSPECT` snippet with a copy button. That expansion is the single highest-leverage piece of text in the whole flow: it converts the most common onboarding failure from a support ticket into a paste.

Degraded rows explain what is missing and what still worked. Degraded is not failure and must not read as failure.

When the first Node succeeds, a persistent card appears at the top:

```
  ✓ You have an inventory.  Try a trace →   [ https://______________ ]
```

That is the thirty-minute payoff, offered the moment it is possible rather than after everything finishes.

---

## 9. Step 7 — First trace

Pre-filled with a hostname discovered during collection, so the operator does not have to think of one. Submitting goes to the Trace Explorer ([trace_explorer.md](trace_explorer.md)).

If it returns a single Hop, an inline note explains why that is a complete and correct answer for a directly-served Site — a one-Hop trace looks like a failure to someone expecting a chain, and the first trace is the worst possible place for that misread.

---

## 10. Error and edge states across the flow

| Situation | Behaviour |
|---|---|
| Master Key file missing on a later start | Server **refuses to start**, prints the expected path, and states it will not generate a replacement. Not a UI state — the process does not come up. |
| Master Key present but malformed | Same refusal, distinct message. |
| Browser reaches the UI before migrations finish | Holding page, polls, then redirects. |
| First-run screen reached twice concurrently | Database uniqueness decides; the loser sees the login page. |
| Node added with no Credential anywhere | Node saves; `Check` returns `422 no_credential` with a link to add one. |
| Two Clusters offer conflicting Credentials | Falls back to the default with a visible warning naming both. The documented hole in the resolution order — [credential.md](../backend/credential.md). |
| Inventory imported before any Credential exists | Allowed. The checklist reorders to put Credential next. |
| All 41 Nodes fail with the same error | Failures grouped by reason with a single explanation and one fix. Forty-one identical error rows are useless. |
| Licence never installed | Community mode: 10 Nodes, full functionality, footer notice. Node 11 still creates, with a banner. Never a hard stop. |
| Operator abandons setup halfway | Every step is independently resumable; the checklist reflects real state on every load, computed rather than stored. |
| Demo mode | Checklist hidden, banner shown, Credential and User management disabled, fixture data present. |

---

## 11. Acceptance criteria

| # | Criterion |
|---|---|
| 1 | Binary start to first Trace in under 30 minutes for a 40-Node fleet, without consulting documentation. |
| 2 | No default password and no admin bypass exists at any point. |
| 3 | The Master Key warning appears in the terminal at generation and in Settings thereafter. |
| 4 | The server refuses to start on a missing or malformed Master Key and never generates a replacement. |
| 5 | Adding a Node performs no connection. |
| 6 | No command runs on a Node before its host key is explicitly approved. |
| 7 | No trust-on-first-use option exists anywhere in the UI. |
| 8 | Bulk host key approval groups by shared fingerprint and shows the count. |
| 9 | A changed host key requires typed confirmation, shows both fingerprints and the prior approval, and is audited. |
| 10 | Inventory import defaults to a dry run showing exactly what would change. |
| 11 | The three reconciliation rules are stated on the import screen. |
| 12 | The Node empty state states that nagipath never scans networks. |
| 13 | The Credential form states the encryption and write-only guarantees inline, not in a collapsible. |
| 14 | Encrypted keys without a passphrase are rejected at save time, not at first connection. |
| 15 | Collection progress shows per-Node results live, with expandable failures containing the failed command and its fix. |
| 16 | The sudo failure case offers a copy-pasteable sudoers snippet. |
| 17 | Degraded outcomes are visually and textually distinct from failures. |
| 18 | Identical failures across many Nodes are grouped by reason. |
| 19 | The "try a trace" prompt appears as soon as the first Node succeeds. |
| 20 | Every step is independently resumable, with checklist state computed from real data. |
