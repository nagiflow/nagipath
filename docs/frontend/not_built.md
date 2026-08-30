# Deliberately not built

The visual source of truth is the `ui/` Storybook (screens `2a`–`2w`); the
functional source of truth is `docs/frontend/*.md`, `docs/backend/*.md` and
the code in `internal/store`.

**When the two disagree, Storybook wins on layout and the code wins on
data.** The design was sketched against a fictional 428-node fleet, so it
shows controls for features that do not exist. Render what the store can
answer. Never invent a field, never fake a number, never ship a control that
does nothing. A panel the backend cannot fill is a panel you leave out, and you
say so in your report.

The full list of sketched controls deliberately absent, so nobody re-adds one
believing it was an oversight:

| Area | Not built, because |
|---|---|
| Every screen | Share, Export ▾, Customize, Views, Saved queries, Save search — nothing persists a view, and a "saved trace" would be a stored result nobody can re-derive. |
| Settings | SAML, external KMS / Vault, per-user cluster scopes, a Service role, API-key scopes / rate limits / IP restriction / request logs. Roles are viewer and admin; the master key is a file on disk. |
| Credentials | Credential testing, "test on N nodes", bastion / jump-host type, an "assigned to" cluster scope, a health column. Credentials resolve per node and have no runtime state. |
| Collection | A schedule editor, bulk collection. One global interval, which the collector actually reads. |
| Host keys | Trust-on-first-use. Every key is decided by a person. |
| Certificates | Chain completeness, OCSP, an externally-reachable flag, "find replacements". The collector parses the leaf; `combined_pem` is the one bundle fact stored. |
| Drift | Reviewed / viewed tracking, "mark node reviewed", file-text diffs, patch export. Drift is computed over parsed objects, so a reformat is not a divergence. |
| Config files | The include tree, file mode / owner / mtime. Rebuilding the tree means reparsing every file per page load. |
| Trace | Snapshot selection, "diff vs previous", headers, "candidates not chosen", graph zoom. A trace always walks the current snapshot. |
| Node › Routes | Shadowed-route detection. Nothing computes shadowing. ("Test a path" is built — it composes a URL and opens Trace.) |
| Node › Sites | Listener / Certificate / State columns on that tab. The `Site` message carries id, primary name, kind, aliases and a route count — nothing per-listener. |
| Rule lookup | A cluster filter, "Advanced query", the plain-language reading of a directive, a group/page-size select. `GetRules` filters by action class and vendor; grouping is always by node. |
| Config search | A context-lines select and the surrounding un-matched lines, a grouping select. The index returns an FTS snippet and a byte offset, not a file window. |
| Sites | A cluster filter. `SiteListRow` has no cluster — a hostname spans clusters. |
| Nodes | "New cluster", moving a node between clusters, and per-instance divergence counts on the cluster band. Clusters are reconciled from collected configuration (`ReconcileClusters`), so the only cluster edits are rename and clear baseline; divergence per member needs `?cluster=` hydration and belongs to the drift report. |
| Collection jobs | The "concurrency N" figure. Nothing in the collector or its defaults sets a concurrency. |
| Import inventory | "last import · <time>". No import record is persisted; the added-nodes panel is what the current import did. |
| Master key | Rotate key, download recovery kit, key history. The key is a file; there is no rotation path. |
| Audit log | A sort select and an actor picker. `GetAudit` takes actor, action, range and page — the actor is typed, not chosen. |
| Every table | Numbered pagers where the endpoint pages by cursor ("Load more" is that cursor), and "Columns · N". |

API keys, collection defaults, retention and the audit log **are** implemented —
they are in this list only where a specific control on them is not.
