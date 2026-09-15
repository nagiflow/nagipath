# Trust-on-first-use for host keys is an explicit, audited, off-by-default setting

[ADR-0011](0011-in-process-ssh-with-stored-credentials.md) says host keys "require explicit
first-use approval in the UI... never blind trust-on-first-use," and several other docs
(`docs/product/access_and_audit.md` §2.3, `docs/backend/credential.md` §5, `docs/backend/node.md`
§4.1, `docs/infra/architecture.md`) repeat that as an absolute — "not even behind a flag." That was
the right default and stays the right default. It stopped being the only thing an operator can have:
some deployments onboard fleets of hosts nobody will individually eyeball a fingerprint for, and
would rather accept a narrower guarantee than run the collector with `must_change_password`-style
policy off entirely.

## Decision

Add one setting, `tofu_enabled` (`internal/store/store.go`'s `settingDefaults`), off by default,
editable only by an admin from Settings › Host keys. When on, `CheckHostKey`
(`internal/store/hostkey.go`) auto-approves a Node's **first-ever** key for a given algorithm
instead of leaving it `pending`. Two things stay true regardless of the setting:

- A key that would **replace** an already-approved one is always a mismatch (`ErrHostKeyMismatch`),
  always left `pending`, and always requires manual approval. This is the actual
  established-host-MITM scenario the earlier docs were arguing against, and the setting never
  touches it.
- Every auto-approval is written to the audit log as `host_key.tofu_approved` (attributed to no
  operator, since none decided it), and flipping the setting itself is audited as
  `hostkey_policy.update` — so a security review of the audit trail can see exactly which keys, if
  any, were never looked at by a person.

## Consequences

The "not even behind a flag" line in the docs above is no longer true and those docs are updated to
say so — the guarantee they were defending (an established host's identity can't be silently
swapped) still holds unconditionally; the guarantee they're not making anymore (every key was ever
seen by a human) is now something a deployment opts into narrowing, not something baked in. Nothing
nudges the setting on: it defaults off, and turning it on is an explicit admin action in a page whose
default view still shows "Approve manually" selected.
