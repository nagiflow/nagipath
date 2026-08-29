import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api, setCSRFToken } from './client'

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
    const fetch = mockFetch({ ok: true })
    vi.stubGlobal('fetch', fetch)

    await api.get('/session')

    const headers = fetch.mock.calls[0][1].headers as Headers
    expect(headers.has('X-CSRF-Token')).toBe(false)
  })

  it('sends the CSRF header on POST', async () => {
    const fetch = mockFetch({ ok: true })
    vi.stubGlobal('fetch', fetch)

    await api.post('/nodes', { name: 'x' })

    const headers = fetch.mock.calls[0][1].headers as Headers
    expect(headers.get('X-CSRF-Token')).toBe('csrf-abc')
  })
})
