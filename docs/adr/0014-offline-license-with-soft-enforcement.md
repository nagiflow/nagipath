# Licensing is an offline signed file with deliberately soft enforcement

nagipath is closed source with no open-source tier, so entitlement is an ed25519-signed license file encoding customer, Node ceiling, expiry and edition, verified offline against a public key embedded in the binary. Phone-home activation was rejected outright: it would break air-gapped operation and contradict the product's core positioning against management planes that require vendor cloud access.

Enforcement is soft. Exceeding the Node ceiling or letting the term lapse produces a loud warning, a banner, an API header and a report entry — it never stops Collection and never deletes data. The reasoning is unglamorous: anyone willing to bypass enforcement can patch it out of a Go binary in an afternoon, so hard enforcement deters nobody who would bypass it, while risking the one unrecoverable outcome — the tool going dark during a customer's incident. The actual ICP does not pirate software; it needs a countersigned agreement and a purchase order. The license file exists to make procurement legitimate and to count Nodes honestly.

## Consequences

A future reader will see enforcement that declines to enforce and assume it is unfinished. It is finished.

Distribution instead relies on a hosted demo seeded from the docker-compose lab: synthetic fleet data, no login, read-only, reset on a schedule, Collection hard-disabled by flag. Because it holds no customer data it carries no compliance burden. Accepting operator-uploaded configuration into that demo was rejected for v1 — it would mean receiving other organisations' internal hostnames and topology on an internet-facing host.
