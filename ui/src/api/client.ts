import { fromJson } from '@bufbuild/protobuf'
import type { JsonValue, MessageShape } from '@bufbuild/protobuf'
import type { GenMessage } from '@bufbuild/protobuf/codegenv2'

// Same-origin session-cookie API client: the cookie carries auth, this only
// needs to attach the CSRF header on mutations and surface a typed error.
// The token itself comes from GET /session (see queries/session.ts) and is
// cached in memory only — it is derived from the session cookie server-side,
// so there is nothing sensitive to protect by hiding it from JS.
let csrfToken = ''

export function setCSRFToken(token: string) {
  csrfToken = token
}

export class APIError extends Error {
  status: number
  code: string

  constructor(status: number, code: string, message: string) {
    super(message)
    this.status = status
    this.code = code
  }
}

async function requestJSON(path: string, init?: RequestInit): Promise<JsonValue> {
  const method = init?.method ?? 'GET'
  const headers = new Headers(init?.headers)
  headers.set('Accept', 'application/json')
  if (method !== 'GET' && method !== 'HEAD') {
    headers.set('X-CSRF-Token', csrfToken)
  }
  const res = await fetch(`/api/ui${path}`, { ...init, method, headers, credentials: 'same-origin' })
  if (res.status === 401) {
    window.location.assign('/login')
    throw new APIError(401, 'unauthenticated', 'Session expired')
  }
  const body = await res.json().catch(() => null)
  if (!res.ok) {
    throw new APIError(res.status, body?.error?.code ?? 'unknown', body?.error?.message ?? res.statusText)
  }
  return body as JsonValue
}

// api.proto.* is every read/write endpoint's wire format now (docs/adr/0018):
// the server marshals with protojson (internal/api/proto.go), and fromJson
// decodes it here against the same generated schema Go built the message
// with — a field renamed in one .proto file changes both sides together,
// nothing hand-typed to drift.
export const api = {
  get: <Desc extends GenMessage<any>>(path: string, schema: Desc): Promise<MessageShape<Desc>> =>
    requestJSON(path).then((json) => fromJson(schema, json)),
  post: <Desc extends GenMessage<any>>(path: string, schema: Desc, body?: unknown): Promise<MessageShape<Desc>> =>
    requestJSON(path, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: body ? JSON.stringify(body) : undefined,
    }).then((json) => fromJson(schema, json)),
  // postAction is for mutations whose response is a plain {ok:true}-shaped
  // envelope rather than a proto message (renames, deletes, decisions) —
  // no schema to decode against, so it stays plain JSON.
  postAction: (path: string, body?: unknown): Promise<{ ok: boolean; [k: string]: unknown }> =>
    requestJSON(path, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: body ? JSON.stringify(body) : undefined,
    }) as Promise<{ ok: boolean; [k: string]: unknown }>,
}
