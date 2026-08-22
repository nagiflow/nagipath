# Config Object identity comes from natural keys, so a rename reads as delete plus create

A Config Object's identity is a natural key per object type: a Site is (Instance, server names, listen address and port), an Upstream or HAProxy frontend/backend is (Instance, name), a Certificate is its fingerprint — with an ordinal tie-breaker when a natural key genuinely collides. Identity based on file path and position was rejected because it breaks whenever a file is moved or reordered, and content hashing was rejected because it breaks on every edit, destroying history. Drift, history and the Trace graph all attach to the identity rather than to the Snapshot, so `first_seen` and `last_seen` live there.

## Consequences

A rename reads as a deletion plus a creation, losing history across the rename. That is accepted: the alternative is fuzzy rename detection, a heuristic that will occasionally merge two unrelated objects, which is a worse failure than a broken history line. If it becomes a real complaint, add an explicit operator action asserting that two identities are the same object rather than inferring it.

Certificates are the easy case — the fingerprint *is* the identity — which is what makes one Certificate resolve across the whole fleet for free.
