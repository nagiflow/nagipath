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

async function request<T>(path: string, init?: RequestInit): Promise<T> {
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
  return body as T
}

export const api = {
  get: <T,>(path: string) => request<T>(path),
  post: <T,>(path: string, json?: unknown) =>
    request<T>(path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: json ? JSON.stringify(json) : undefined }),
}
