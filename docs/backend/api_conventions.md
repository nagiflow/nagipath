# API Conventions

Stated once here; the entity documents in this directory assume all of it and do not restate it.

## Base and versioning

All endpoints live under `/api/v1`. The UI and `nagipath ctl` consume the **same** handlers — content negotiation by `Accept` header selects HTML fragments (htmx) or JSON. There is no second API and no CLI-only path into the database, so the two cannot drift apart.

## Authentication

| Client | Mechanism |
|---|---|
| Browser | Session cookie: `Secure`, `HttpOnly`, `SameSite=Strict`. Token is random 32 bytes; only its SHA-256 is stored (`user_session.token_hash`). |
| `nagipath ctl` | `Authorization: Bearer <token>` against `api_token`. Token shown once at creation, stored hashed. |

Unauthenticated requests get `401`. Authenticated-but-unauthorised get `403` **and** an `audit_event` row with `outcome='denied'`.

## Authorization

Every handler calls `authz.Can(user, action, target)`. No handler reads `user.Role` directly. v1 resolves to two roles:

| | admin | viewer |
|---|---|---|
| Read anything | ✓ | ✓ |
| Run a Probe | ✓ | ✗ |
| Trigger a Collection | ✓ | ✗ |
| Mutate Nodes, Clusters, Credentials, Applications, settings | ✓ | ✗ |
| Approve a Host Key | ✓ | ✗ |
| View Credential secrets | **✗ — nobody** | ✗ |

The single chokepoint is what makes v1.1's Application-scoped RBAC a filter in one place rather than an audit of every query.

## Errors

One envelope, always:

```json
{
  "error": {
    "code": "host_key_not_approved",
    "message": "Node web02.corp.example has an unapproved SSH host key.",
    "detail": { "node_id": 12, "fingerprint": "SHA256:qX8…" },
    "request_id": "01J8Z2K4M7"
  }
}
```

`code` is a stable machine-readable string — safe to branch on. `message` is for humans and may change. `request_id` matches the structured log line.

| Status | Used for |
|---|---|
| `400` | Malformed request. |
| `401` / `403` | Unauthenticated / unauthorised. |
| `404` | Not found, or found but not visible to this user. |
| `409` | State conflict — a Collection already running for this Node, a duplicate natural key. |
| `422` | Well-formed but semantically invalid — a bastion cycle, an Entry Point with no hostname. |
| `429` | Rate limit (Probe and login only). |
| `503` | Datastore unavailable or migrations in progress. |

## Pagination and filtering

Cursor-based, never offset — offset pagination over a table being written by a Collection skips and repeats rows.

```
GET /api/v1/nodes?limit=50&cursor=eyJpZCI6MTIzfQ&vendor=nginx&drifted=true&q=web
```

```json
{ "items": [ … ], "next_cursor": "eyJpZCI6MTczfQ", "total_estimate": 412 }
```

`limit` defaults to 50, caps at 500. `total_estimate` is explicitly an estimate; exact counts on a fleet-wide filtered query are not worth the scan.

## Mutation semantics

- Mutating requests require a CSRF token when session-authenticated, including htmx requests.
- Create endpoints accept an optional `Idempotency-Key` header; a repeat within 24h returns the original result rather than a duplicate.
- Long operations (`POST /nodes/{id}/collect`, `POST /probes`) return `202 Accepted` with a resource to poll, never a held-open connection.
- Every mutation writes its `audit_event` in the **same transaction** as its effect.

## Timestamps and IDs

ISO-8601 UTC strings with `Z`. IDs are integers, opaque to clients — natural keys are for identity across Snapshots, not for URLs.

## What is never in a response

Private key material in any form, `credential.private_key_ct` or its plaintext, the Master Key, session or API token values after creation, and certificate or key file bytes. These are excluded at the serialiser, not by remembering to omit them per handler.
