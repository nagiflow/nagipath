import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api, setCSRFToken } from './client'
import { SessionResponseSchema } from './pb/nagipath/api/v1/session_pb'

function mockFetch(body: unknown, status = 200) {
  return vi.fn().mockResolvedValue({
    ok: status < 400,
    status,
    json: () => Promise.resolve(body),
  })
}

describe('api client', () => {
  beforeEach(() => {
    setCSRFToken('csrf-abc')
  })

  it('does not send a CSRF header on GET', async () => {
    const fetch = mockFetch({})
    vi.stubGlobal('fetch', fetch)

    await api.get('/session', SessionResponseSchema)

    const headers = fetch.mock.calls[0][1].headers as Headers
    expect(headers.has('X-CSRF-Token')).toBe(false)
  })

  it('sends the CSRF header on POST', async () => {
    const fetch = mockFetch({})
    vi.stubGlobal('fetch', fetch)

    await api.postAction('/nodes', { name: 'x' })

    const headers = fetch.mock.calls[0][1].headers as Headers
    expect(headers.get('X-CSRF-Token')).toBe('csrf-abc')
  })
})
