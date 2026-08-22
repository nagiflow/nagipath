# Rules are uniform records with an Action Class, not a typed schema per directive

Rules are stored in one shape — scope, order index, directive name, raw text, arguments, condition, Action Class, Provenance — and classified into a closed taxonomy of roughly ten Action Classes rather than modelled with a bespoke schema per directive. Typed schemas for rewrite rules, headers, caching, ACLs, auth and rate limiting would be hundreds of schemas, each a parser branch, a migration and something that breaks on the next release or the next custom module. The uniform shape means an unknown or custom-module directive still lands in the correct scope, in the correct order, with its raw text and an Action Class of `other`, so coverage is automatic instead of a treadmill.

## Consequences

Rules can be displayed, ordered, grouped, filtered and full-text searched, but not reasoned about semantically — "is HSTS correctly configured?" needs typed knowledge this deliberately lacks. Questions of that shape are answered by full-text search across Snapshots, which handles an unbounded class of them for a fixed cost, rather than by growing the typed model.

Typed enrichment is layered on top for a deliberately tiny set that the Trace must follow: proxy and upstream directives, TLS references, and eventually the auth directives that SSO support needs.
