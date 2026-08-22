# Self-managed only; SaaS deferred behind five unlocked doors

nagipath ships as software customers run themselves. A GCP-hosted multi-tenant SaaS was considered and rejected for now, because a control plane in our cloud can never dial into a segmented enterprise network — so every SaaS customer would install an in-network connector anyway, meaning SaaS buys no reduction in install friction while adding tenant isolation, SOC 2, on-call, and custody of credentials with root-equivalent reach into thousands of web servers. The ICP is also structurally hostile to it: an organisation still running 1,000 on-prem web servers usually has a regulatory or segmentation reason, and that reason correlates almost perfectly with refusing a vendor cloud management plane.

## Considered Options

- **Self-managed only, SaaS-capable** (chosen)
- **Both from day one** — pays the tenancy, compliance and on-call cost immediately
- **Self-managed plus vendor-hosted single-tenant instances** — same engineering as the chosen option, purely a commercial posture; adopt if a customer asks for "hosted"

## Consequences

Five constraints keep SaaS a later deployment mode rather than a rewrite, and cost almost nothing now:

1. An organisation scope exists in the data model from the first migration (the RBAC scope tree already needs it).
2. All target connectivity goes through one narrow executor interface, so an outbound-connecting gateway is a second implementation rather than a new transport.
3. The control plane makes no assumptions about local filesystem layout beyond its own data file.
4. Configuration comes from flags and environment as well as file; server processes hold no session state.
5. Entitlement checks sit behind an interface, with the offline license file as the only implementation.

Choosing SQLite (ADR-0003) narrows door 1 rather than leaving it free: multi-instance HA or SaaS would require a SQL port. That is accepted deliberately.
