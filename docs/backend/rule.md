# Rule and Action Class

**Schema:** [schema.md §8](schema.md#8-rules-and-search). **Conventions:** [api_conventions.md](api_conventions.md).
**Decisions:** ADR-0006 (uniform Rule records, not typed schemas), ADR-0005 (Provenance).

---

## 1. Overview

A **Rule** is one ordered directive within a scope, carrying its raw text, its Action Class and its Provenance. **Unmodelled and custom directives are still Rules.**

That last sentence is the entire design. There are tens of thousands of possible directives across NGINX, Apache, HAProxy and their module ecosystems, and every enterprise fleet contains directives from modules we have never heard of. The alternative designs both fail:

- **A typed schema per directive** means a schema treadmill: every customer's unusual module is a migration, a parser change and a release. Coverage is always behind, and the gaps are silent.
- **Not parsing directives at all** means the developer self-service and Rule-lookup features cannot exist, and the admin gets a text dump they could have got from `cat`.

So: **one uniform shape, one closed taxonomy.** An unrecognised directive still becomes a Rule with the correct scope, the correct order and `action_class='other'`, `is_modelled=0`. Coverage of a new module is automatic. Depth is what improves over time, never breadth (ADR-0006).

---

## 2. Action Class

A closed ten-value taxonomy. Closed is the point — an open list becomes a tag soup where nobody can filter reliably.

| Class | Means | Examples |
|---|---|---|
| `match` | Selects whether subsequent Rules apply | `if`, `RewriteCond`, HAProxy `acl` |
| `rewrite` | Changes the request path or URI | `rewrite`, `RewriteRule`, `http-request set-path`, `http-request replace-path` |
| `redirect` | Returns a 3xx to the client | `return 301`, `Redirect`, `RedirectMatch`, `http-request redirect` |
| `header` | Sets, removes or modifies a header | `add_header`, `proxy_set_header`, `Header set`, `RequestHeader`, `http-request set-header` |
| `auth` | Requires or delegates authentication/authorisation | `auth_basic`, `auth_request`, `AuthType`, `Require`, `mod_auth_openidc`, PingAccess agent directives |
| `cache` | Controls caching | `proxy_cache`, `expires`, `CacheEnable`, `ExpiresByType` |
| `rate_limit` | Limits request rate or connections | `limit_req`, `limit_conn`, HAProxy `stick-table` + `http-request deny` on rate |
| `proxy` | Configures how a request is forwarded | `proxy_pass` options, `proxy_read_timeout`, `ProxyPreserveHost`, `option forwardfor` |
| `access_control` | Allows or denies by client attribute | `allow`, `deny`, `Require ip`, HAProxy `http-request deny` |
| `other` | Everything else, including every unmodelled directive | `error_page`, `client_max_body_size`, any third-party module |

Classification uses a directive-name lookup table per Vendor. A directive absent from the table gets `other` and `is_modelled=0`.

`auth` earns its place ahead of its v1 usefulness. Access-management layers are the strongest planned differentiator, and PingAccess or SiteMinder agent modules inside Apache and IIS appear as ordinary directives. Classifying them as `auth` from v1 means the v2 SSO work is a lookup-table extension plus a UI treatment, not a data-model change.

---

## 3. `is_modelled`

| | `is_modelled=1` | `is_modelled=0` |
|---|---|---|
| Directive is in the per-Vendor table | ✓ | |
| Arguments normalised into `args` | ✓ | `args` is the raw argument string |
| Semantics understood well enough to reason about | ✓ | |
| Appears in Rule lookup, search, Drift, `hop_rule` | ✓ | ✓ |

This flag is what lets the UI distinguish *"we understand this directive"* from *"here it is verbatim, in the right scope and the right order"*. Both are useful; conflating them is how a tool ends up quietly wrong.

An **Opaque Directive** needs no table of its own: it is a Rule with `is_modelled=0`, plus the raw bytes already in `snapshot_file`.

---

## 4. Scope and order

`(scope_kind, scope_id, ordinal)` places a Rule. `scope_kind` is one of `global`, `http`, `site`, `route`, `upstream`, `listener`.

**Order is semantics.** Two configurations with identical Rule sets in different order behave differently, which is why `ordinal` is part of the natural key, why `drift_finding.change` includes `reordered`, and why nothing in the API ever returns Rules unordered.

### 4.1 Inheritance

A Route's *effective* Rules include those of its ancestor scopes. Apache makes this unavoidable — directives in an outer `<Directory>` apply within inner ones, and `RewriteRule` inheritance is governed by `RewriteOptions Inherit`. NGINX has its own version: `add_header` in a `server` block applies in a `location` **unless** that `location` declares its own `add_header`, at which point the outer ones are discarded entirely. That NGINX rule surprises experienced admins and is a frequent cause of missing security headers, so it is modelled explicitly rather than approximated by concatenation.

Effective Rules are therefore **computed, not stored**: `hop_rule` records the resolved set for a Trace with `inherited_from_route_id` naming where each came from. Storing a materialised effective set per Route would multiply rows by nesting depth and go stale the moment inheritance logic improved.

---

## 5. Rule lookup by context path

The developer self-service surface, and per PRD-V1 §3.1 it ships with the Trace pillar.

**Input:** hostname + path prefix. **Output:** the ordered list of Candidate Rules across every Instance that could serve it, grouped by Instance and Action Class.

```
1. Match hostname against site_name across all current Snapshots
   (exact → wildcard prefix → wildcard suffix → regex → catch-all)
2. For each candidate Site, select Routes matching the path in Vendor precedence order
3. For each selected Route, resolve effective Rules including inheritance
4. Group by Instance, order by scope then ordinal, tag with Action Class
5. Every Rule is Candidate — configuration says it could apply, nothing confirms it did
```

Every Rule returned is `candidate`, and the API says so on every row. The distinction is the product's honesty commitment: **configuration says these could apply; only a Probe correlated to an access log can say one did** ([probe.md](probe.md)).

This is also where the confidence vocabulary pays for itself. A raw list of eleven possibly-matching Rules is what an admin already gets from `grep` and is not useful — the earlier design that stopped there was rejected for exactly that reason. The list is the starting point; verification is what makes it an answer.

---

## 6. Search

Two FTS5 indexes, both with a custom tokenizer splitting on `/.:-_` — every real query here is a path, hostname or directive, and the default tokenizer treats `api/v2` and `payments.corp.example` as single opaque tokens.

| Index | Covers | Why |
|---|---|---|
| `rule_fts` | Rule text across **all** Snapshots | External-content FTS5 over `rule`; cheap, and history matters for "when did this header appear". |
| `snapshot_text_fts` | Raw configuration text of **current** Snapshots only | FTS5 needs plaintext, so indexing history would store an uncompressed copy of every historical file alongside the compressed one — multiplying the database by retention depth for a query nobody has asked for. |

Search is the escape hatch for questions the model does not anticipate. "Which vhosts have `client_max_body_size` over 100m", "who still references the retiring CA", "where is `mod_auth_openidc` configured" — all answerable without growing the typed model, which is precisely what ADR-0006 buys.

The current-only scope is a **documented deviation** from PRD-V1 §3.2's unqualified promise of search "across raw Snapshots", carried as a finding in [schema.md §15](schema.md#15-findings-for-the-decision-record) and returned as `scope_note` in every search response.

---

## 7. API

Rules are read-only in v1.

### `GET /api/v1/rules`

Filters: `instance_id`, `site_id`, `route_id`, `action_class`, `directive`, `is_modelled`, `cluster_id`, `q`.

```json
{
  "items": [
    {
      "id": 55021,
      "instance": { "id": 301, "vendor": "nginx", "display_name": "nginx (edge)" },
      "scope": { "kind": "route", "id": 9001, "label": "payments.corp.example  location /api" },
      "ordinal": 4,
      "directive": "proxy_set_header",
      "action_class": "header",
      "args": "X-Forwarded-Proto $scheme",
      "raw_text": "    proxy_set_header X-Forwarded-Proto $scheme;",
      "is_modelled": true,
      "provenance": { "snapshot_file_id": 41002, "path": "/etc/nginx/conf.d/api.conf",
                      "line": 24, "byte_start": 731, "byte_end": 779 }
    }
  ],
  "next_cursor": null
}
```

`raw_text` preserves original indentation. That is not cosmetic — it is the byte range patch-in-place editing will operate on.

### `GET /api/v1/rules/lookup`

The developer-facing endpoint.

```
GET /api/v1/rules/lookup?hostname=payments.corp.example&path=/api/v2/charge
```

```json
{
  "query": { "hostname": "payments.corp.example", "path": "/api/v2/charge", "scheme": "https" },
  "confidence": "candidate",
  "confidence_note": "Configuration says these Rules could apply. Run a Probe to confirm which did.",
  "instances": [
    {
      "instance": { "id": 302, "vendor": "haproxy", "display_name": "haproxy (edge)",
                    "node": "lb01.corp.example" },
      "site": { "id": 8100, "primary_name": "fe_https", "matched_by": "acl hdr_beg(host)" },
      "matched_routes": [
        { "id": 9100, "match_type": "haproxy_acl_use_backend", "pattern": "path_beg /api",
          "precedence_rank": 90,
          "precedence_explanation": "HAProxy use_backend rules are evaluated in file order; this is the first match.",
          "upstream": { "id": 6100, "name": "be_payments" } }
      ],
      "rules_by_class": {
        "header": [ { "id": 55900, "directive": "http-request set-header",
                      "args": "X-Forwarded-Proto https", "ordinal": 2, "is_modelled": true,
                      "inherited_from": null,
                      "provenance": { "path": "/etc/haproxy/haproxy.cfg", "line": 88 } } ],
        "auth":   [ { "id": 55901, "directive": "http-request auth",
                      "args": "realm corp unless { src 10.0.0.0/8 }", "ordinal": 3,
                      "is_modelled": true, "inherited_from": null,
                      "provenance": { "path": "/etc/haproxy/haproxy.cfg", "line": 89 } } ],
        "other":  [ { "id": 55902, "directive": "option", "args": "httplog", "ordinal": 1,
                      "is_modelled": false,
                      "provenance": { "path": "/etc/haproxy/haproxy.cfg", "line": 84 } } ]
      },
      "undetermined": [
        { "route_id": 9101, "reason": "ACL expression not evaluable by the trace engine",
          "raw_text": "use_backend be_canary if { hdr_sub(cookie) canary=1 }" }
      ]
    },
    {
      "instance": { "id": 301, "vendor": "nginx", "display_name": "nginx (edge)",
                    "node": "web02.corp.example" },
      "site": { "id": 8001, "primary_name": "payments.corp.example", "matched_by": "exact server_name" },
      "matched_routes": [
        { "id": 9001, "match_type": "prefix", "pattern": "/api", "precedence_rank": 40,
          "precedence_explanation": "Longest matching prefix location. No exact (=) or ^~ location matched, and no regex location matched first.",
          "upstream": { "id": 6001, "name": "payments_api" } }
      ],
      "rules_by_class": {
        "rewrite": [ { "id": 55010, "directive": "rewrite", "args": "^/api/v2/(.*) /$1 break",
                       "ordinal": 1, "is_modelled": true, "inherited_from": null,
                       "provenance": { "path": "/etc/nginx/conf.d/api.conf", "line": 19 } } ],
        "header":  [ { "id": 55021, "directive": "proxy_set_header",
                       "args": "X-Forwarded-Proto $scheme", "ordinal": 4, "is_modelled": true,
                       "inherited_from": null,
                       "provenance": { "path": "/etc/nginx/conf.d/api.conf", "line": 24 } } ]
      },
      "shadowed": [
        { "id": 55004, "directive": "add_header", "args": "Strict-Transport-Security max-age=31536000",
          "scope": "site",
          "reason": "location /api declares its own add_header, which discards all add_header directives inherited from the server block" }
      ],
      "undetermined": []
    }
  ],
  "unmatched_reason": null
}
```

Three fields here do most of the work, and none of them is the Rule list.

`precedence_explanation` justifies the ordering on screen, because users will disbelieve it otherwise — "why is my exact location losing to a prefix" is the support question this pre-empts.

`shadowed` catches NGINX's `add_header` discard rule. A missing security header caused by an inner `add_header` is a genuinely hard bug to find by reading configuration, and surfacing it is one of the clearest demonstrations that the tool understands the Vendor rather than just reading its files.

`undetermined` is what stops the answer from being a lie. Where an ACL or condition cannot be evaluated, the branch is listed verbatim with the reason, rather than dropped or assumed false.

### `GET /api/v1/rules/{id}`

One Rule plus its scope chain, its ancestor scopes, and every other Instance in the same Cluster carrying an equivalent Rule — the fleet-consistency question, answered from the single-Rule view.

### `GET /api/v1/rules/distribution`

Fleet-wide aggregate for the inventory dashboard.

```json
{ "by_action_class": { "header": 4210, "proxy": 3880, "rewrite": 1102, "auth": 96, "other": 8814 },
  "unmodelled_top_directives": [ { "directive": "lua_shared_dict", "count": 44, "instances": 12 } ],
  "note": "unmodelled directives are recorded in correct scope and order with action_class='other'" }
```

`unmodelled_top_directives` is our own product backlog, generated from customer reality: it is the ranked list of which directives to model next, measured rather than guessed.

---

## 8. Edge cases

| Case | Behaviour |
|---|---|
| Directive from an unknown third-party module | Rule with `action_class='other'`, `is_modelled=0`, correct scope and order. Appears in search, lookup and Drift. |
| Same directive twice in one scope | Two Rules, distinguished by `ordinal`. Frequently a real bug and worth surfacing, so never deduplicated. |
| NGINX `add_header` in both `server` and `location` | Inner wins and **discards all outer ones**; the outer Rules are returned in `shadowed` with that explanation. |
| Apache `RewriteRule` in a parent `<Directory>` | Included with `inherited_from_route_id` set, subject to `RewriteOptions`. |
| Multi-line directive with a line continuation | One Rule; `raw_text` and the byte range span all lines. |
| Directive inside an unresolved `<If>` (degraded Snapshot only) | Rule recorded with the condition attached and marked unresolved. Never assumed true. |
| Rule text over 1 MB (generated `map` blocks) | Stored; excluded from FTS with a note. Indexing a megabyte of generated key-value pairs serves nobody. |
| Lookup hostname matches nothing | `200` with empty `instances` and a populated `unmatched_reason` — `no_matching_site_name`, `no_listener_on_port`, `all_snapshots_unparsed`. Never an empty `200` with no explanation. |
| Lookup path matches only a `catch_all` Site | Returned, with `matched_by='catch_all'` and a note that no named Site claimed the hostname. |
| Vendor renames a directive across versions | Both names in the classification table; `directive` stores what the file actually says. |
| Parser upgrade reclassifies a directive | Only current Snapshots are re-parsed; a diff re-parses both sides, so reclassification cannot manufacture Drift (ADR-0012). |
