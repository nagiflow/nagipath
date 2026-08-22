# Config Objects — Listener, Site, Route, Upstream, Upstream Member

**Schema:** [schema.md §7](schema.md#7-derived-topology), [§1.1 derived-row contract](schema.md#11-the-derived-row-contract). **Conventions:** [api_conventions.md](api_conventions.md).
**Decisions:** ADR-0005 (position-preserving parse), ADR-0007 (natural-key identity), ADR-0004 (vendor tooling authority).

---

## 1. Overview

A **Config Object** is a single normalized element parsed out of a Snapshot. Five kinds carry topology:

| Kind | Definition | NGINX | Apache | HAProxy |
|---|---|---|---|---|
| **Listener** | address, port, TLS posture | `listen` | `Listen` + `<VirtualHost addr:port>` | `bind` in a frontend |
| **Site** | the Vendor's host-level routing object | `server` block | `<VirtualHost>` | `frontend` |
| **Route** | a path or condition within a Site selecting a destination | `location` | `<Directory>`, `<Location>`, `<Files>`, `Alias`, `ProxyPass` | `use_backend` / `default_backend` |
| **Upstream** | a named group of destinations | `upstream`, inline `proxy_pass` | `<Proxy balancer://>`, inline `ProxyPass` | `backend` |
| **Upstream Member** | one destination in an Upstream | `server` in `upstream` | `BalancerMember` | `server` in `backend` |

These five are what a Trace walks. They are the deliberately **modelled** part of configuration; everything else is a Rule ([rule.md](rule.md)) or an Opaque Directive.

All five obey the derived-row contract: they are produced by parsing, carry `parser_version`, `natural_key`, `ordinal` and Provenance, and can be deleted and rebuilt from the Snapshot with no loss.

---

## 2. Provenance is not optional

Every Config Object records `(prov_file_id, prov_byte_start, prov_byte_end)` — the exact bytes in the exact file it was parsed from.

Two reasons, and only the second is about v1.

The immediate one: an operator looking at a Route in the UI must be one click from the configuration that produced it. Without that, every finding ends in "…now go SSH in and find it yourself", which is the workflow the product exists to replace.

The structural one: byte-accurate Provenance is what makes future editing **patch-in-place rather than generate**. Editing by regenerating configuration from a model means imposing our formatting on every customer's file, which is unacceptable in a fleet nobody has agreed to hand over. Patching the exact byte range preserves their comments, ordering and style because it literally is their text. Recording Provenance now is nearly free; retrofitting it means rewriting all three parsers (ADR-0005).

The parsers are therefore **position-preserving concrete syntax trees**, not abstract syntax trees. Whitespace, comments and original token text survive parsing.

---

## 3. Parsing per Vendor

Input is the stored Snapshot, and the primary text is the vendor dump, which has already resolved includes and conditionals (ADR-0004). Provenance is mapped back to the real file paths recovered from the dump's banners.

### 3.1 NGINX

Grammar is a clean nested block structure, so the CST is straightforward. What is not straightforward:

- **`server_name` multiplicity.** One `server` block declares many names with different match semantics — exact, leading wildcard `*.example.com`, trailing wildcard `www.*`, regex `~^`. Hence `site_name` as a child table with a `match_kind`, not a delimited string in a column.
- **Default server.** `listen … default_server`, or absent that, the first `server` for a given `address:port`. `listener.is_default` records which, because it decides where an unmatched hostname lands — a question operators ask constantly.
- **Inline `proxy_pass`.** `proxy_pass http://10.0.0.5:8080` with no `upstream` block still has a destination. Synthesised as an `upstream` with `kind='nginx_inline'` and one member, so the Trace walk has something uniform to follow.
- **`proxy_pass` trailing slash.** `proxy_pass http://api/` strips the matched location prefix; `proxy_pass http://api` does not. This changes the path the next Hop receives and is recorded in the Route so `hop.effective_path` is right.

### 3.2 Apache

The hardest of the three:

- **Section types with different precedence.** `<Directory>`, `<DirectoryMatch>`, `<Files>`, `<FilesMatch>`, `<Location>`, `<LocationMatch>`, `<Proxy>`, `<If>`. All become Routes with distinct `match_type` and `precedence_rank`.
- **`<Directory>` is filesystem-scoped, not URL-scoped.** Mapping it to a request path needs `DocumentRoot` plus `Alias` and `ScriptAlias`. Where the mapping is ambiguous, the Route is recorded with the filesystem pattern and the UI says the URL mapping is uncertain rather than inventing one.
- **Inheritance.** Directives in an outer section apply inside inner ones, and `RewriteRule` inheritance depends on `RewriteOptions`. Hence `route.parent_route_id`, and hence `hop_rule` as a table rather than a join on `route_id`.
- **`ServerName` + `ServerAlias`** map to `site_name` rows, same as NGINX's `server_name`.
- **`ProxyPass` / `ProxyPassMatch` / `<Proxy balancer://>`** produce Upstreams; `BalancerMember` produces Members. A bare `ProxyPass /x http://host:port/` synthesises an `apache_inline` Upstream.

### 3.3 HAProxy

Structurally the simplest and the only Vendor where **completeness is provable**: there is no `include`, so the process's `-f` arguments are the complete file set.

- `frontend` → Site, `bind` → Listener, `backend` → Upstream, `server` → Member.
- Routing is `acl` definitions plus `use_backend <name> if <acl>`, evaluated in file order, with `default_backend` last. Ranks 90 and 99.
- The ACL condition itself is a Rule with `action_class='match'`, referenced by the Route. Its expression can be arbitrary (`hdr_beg(host)`, `path_beg`, `src`, boolean combinations), so it is preserved verbatim and evaluated only for the subset the Trace engine understands. What it cannot evaluate, it says.
- `listen` sections are both frontend and backend; they produce one Site *and* one Upstream sharing a natural key prefix.

---

## 4. Route precedence

The single most important piece of correctness in the product, because **rule evaluation order is not file order** and a Trace that follows file order is confidently wrong.

Precedence is resolved at parse time into `route.precedence_rank` plus `route.specificity`, so the Trace engine is `ORDER BY precedence_rank, specificity DESC, ordinal` with no vendor conditionals in the hot path. The rank table is in [schema.md §7.1](schema.md#71-precedence_rank-real-vendor-semantics-computed-once).

Two subtleties encoded there that are easy to get backwards:

- **NGINX regex locations match in file order**, and the *first* match wins. So at rank 30, `specificity` is ignored and `ordinal` decides — the opposite of the longest-prefix rule one rank below at rank 40.
- **`^~` suppresses regex evaluation entirely.** A matching `^~` prefix at rank 20 means ranks 30 and 40 are never consulted. This is not a scoring adjustment; it is a short-circuit, and the Trace engine implements it as one.

Apache's ordering is by section type, and within `<Directory>` by path length **shortest to longest**, because later-applied directives override earlier ones. That is the inverse of NGINX's longest-prefix-wins, which is exactly why this is computed once at parse time rather than reasoned about at query time.

---

## 5. Identity and change detection

Natural keys per [schema.md §4](schema.md#4-object-identity). Consequences that show up in the API:

- Renaming an Upstream reads as **one removed and one added**. Fuzzy rename detection was declined (ADR-0007): it produces confident wrong answers, and "removed `api_v1`, added `api_v2`" is a true statement an operator can interpret.
- Reordering Routes with unchanged text is a real change, because order is semantics. `drift_finding.change` has a `reordered` value for exactly this.
- Two Routes with the same natural key in one Site are legal (duplicated `location` blocks happen); `ordinal` distinguishes them, and the duplication is itself worth surfacing.

---

## 6. API

Config Objects are read-only. There is no `POST` or `PATCH` anywhere in this document — they are derived, and the only way to change one is to change the configuration on the Node.

### `GET /api/v1/instances/{id}/topology`

The whole tree for the current Snapshot, or `?snapshot_id=`.

```json
{
  "snapshot_id": 99120,
  "parser_version": 7,
  "degraded": false,
  "listeners": [
    { "id": 7001, "address": "0.0.0.0", "port": 443, "tls": true, "protocol": "http2",
      "is_default": true, "raw_text": "listen 443 ssl http2 default_server;",
      "provenance": { "path": "/etc/nginx/conf.d/api.conf", "line": 3, "byte_start": 61, "byte_end": 100 } }
  ],
  "sites": [
    {
      "id": 8001, "kind": "nginx_server", "primary_name": "payments.corp.example",
      "listener_ids": [7001],
      "names": [ { "name": "payments.corp.example", "match_kind": "exact" },
                 { "name": "*.payments.corp.example", "match_kind": "wildcard_prefix" } ],
      "route_count": 12,
      "provenance": { "path": "/etc/nginx/conf.d/api.conf", "line": 2, "byte_start": 40, "byte_end": 3120 }
    }
  ],
  "routes": [
    { "id": 9001, "site_id": 8001, "parent_route_id": null,
      "match_type": "prefix", "pattern": "/api", "precedence_rank": 40, "specificity": 4,
      "upstream_id": 6001, "target_raw": "proxy_pass http://payments_api;",
      "is_terminal": true, "rule_count": 7,
      "provenance": { "path": "/etc/nginx/conf.d/api.conf", "line": 18, "byte_start": 512, "byte_end": 980 } }
  ],
  "upstreams": [
    { "id": 6001, "name": "payments_api", "kind": "nginx_upstream", "balance_method": "least_conn",
      "members": [
        { "id": 5001, "host": "app01.corp.example", "port": 8080, "scheme": "http",
          "weight": 1, "flags": "",
          "resolution": { "addresses": ["10.20.3.11"], "method": "getent_hosts",
                          "resolved_on_node_id": 12, "resolved_at": "2026-08-21T09:14:01Z",
                          "managed_instance_id": 402 } },
        { "id": 5002, "host": "app02.corp.example", "port": 8080, "scheme": "http",
          "weight": 1, "flags": "backup",
          "resolution": { "addresses": [], "method": "unresolved",
                          "resolved_on_node_id": 12, "managed_instance_id": null } }
      ] }
  ]
}
```

Every object carries `provenance` with a human-usable `path` and `line` alongside the byte range. `resolution` is attached at read time from `dns_resolution` and carries `resolved_on_node_id`, because *which Node resolved the name* is part of the answer under split-horizon DNS (ADR-0010).

### `GET /api/v1/sites`

Fleet-wide. Filters: `q`, `hostname` (matches `site_name` including wildcard semantics), `vendor`, `cluster_id`, `has_tls`, `port`. This is the endpoint behind "who serves `payments.corp.example`?", which is the second question every operator asks after the Trace.

### `GET /api/v1/routes?site_id=…&ordered=true`

Returns Routes in **evaluation order**, not file order, with `precedence_rank` and a short `precedence_explanation` per row (`"NGINX exact match — evaluated before all prefix and regex locations"`). The explanation is generated, not stored, and exists because the ordering is the part users will disbelieve until it is justified on screen.

### `GET /api/v1/upstreams` · `GET /api/v1/upstreams/{id}`

Fleet-wide with `member_host` filter — "which Upstreams point at `app01`?" is the reverse-impact question asked before decommissioning a host.

### `GET /api/v1/dns-disagreements`

```json
{ "items": [ { "name": "app01.corp.example",
               "resolutions": [ { "node_id": 12, "addresses": ["10.20.3.11"] },
                                { "node_id": 44, "addresses": ["10.90.3.11"] } ],
               "affected_upstream_member_ids": [5001, 5188] } ] }
```

A **finding, not an error**. Split-horizon DNS is normal; two Nodes disagreeing about one name is either intentional or a serious misconfiguration, and only the operator knows which. nagipath surfaces it and takes no position.

---

## 7. Edge cases

| Case | Behaviour |
|---|---|
| `server` block with no `server_name` | `site_name` row with `match_kind='catch_all'`. Matches only when no named Site does, and only on its Listener. |
| Two Sites claim the same name on the same `address:port` | Both recorded. First wins at request time; the UI flags the shadowed one — a real and commonly unnoticed misconfiguration. |
| `listen` with only a port | `address='0.0.0.0'`. |
| Unix socket `listen` | `address` holds the socket path, `port` is `NULL`. Reachable only locally, which the Trace states when a Hop's next step is a socket on a different Node. |
| `include` pulling a file that no longer exists | The vendor dump already resolved it, so the Snapshot is complete. In degraded fallback mode, the missing include is recorded as unresolved. |
| Apache `<Directory>` with no URL mapping | Route stored with the filesystem pattern; URL mapping marked uncertain rather than invented. |
| `<If>` / `IfModule` conditional | Vendor dump resolves it. In fallback mode, both branches are recorded as Rules with the condition attached and marked unresolved. Never assumed true. |
| HAProxy `listen` section | One Site and one Upstream from a single block. |
| HAProxy ACL the engine cannot evaluate | Route retained; condition preserved verbatim; Trace marks that branch undetermined and lists it. |
| `proxy_pass` to a variable (`proxy_pass http://$backend`) | Upstream recorded with `target_raw` and no resolvable member. Trace ends in an External Hop with reason `unresolvable_upstream` — honest, since the destination genuinely depends on runtime state. |
| `proxy_pass` trailing-slash difference | Recorded on the Route; changes `hop.effective_path` for the next Hop. |
| Upstream Member is a managed Instance on another Node | The Trace continues to it. This is the multi-hop case the product exists for. |
| Member host resolves to an address no managed Node has | External Hop naming the address and the reason. |
| Snapshot with `parse_state='failed'` | No Config Objects. Instance shows unparsed, raw dump linked, Trace refuses to run against it rather than returning an empty answer. |
