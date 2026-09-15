import { describe, expect, it } from 'vitest'
import { parseRuleQuery, stringifyRuleQuery, type RuleQuery } from './kql'

function ok(input: string): RuleQuery {
  const r = parseRuleQuery(input)
  if ('error' in r) throw new Error(`expected ${input} to parse, got error: ${r.error}`)
  return r.query
}
function err(input: string): string {
  const r = parseRuleQuery(input)
  if (!('error' in r)) throw new Error(`expected ${input} to fail, got ${JSON.stringify(r.query)}`)
  return r.error
}

describe('parseRuleQuery', () => {
  it('parses an empty query', () => {
    expect(ok('')).toEqual({ hostname: '', path: '', scheme: '', port: '', classes: [], vendors: [] })
  })

  it('parses a single field:value term', () => {
    expect(ok('hostname:payments.corp.example')).toMatchObject({ hostname: 'payments.corp.example' })
  })

  it('parses fields joined with and', () => {
    expect(ok('hostname:payments.corp.example and path:/api/v2/charge')).toMatchObject({
      hostname: 'payments.corp.example', path: '/api/v2/charge',
    })
  })

  it('parses a quoted value with spaces', () => {
    expect(ok('path:"/api/v2/charge now"')).toMatchObject({ path: '/api/v2/charge now' })
  })

  it('parses or between repeated facet values', () => {
    expect(ok('vendor:nginx or vendor:haproxy')).toMatchObject({ vendors: ['nginx', 'haproxy'] })
  })

  it('parses a parenthesized facet group combined with and', () => {
    expect(ok('(vendor:nginx or vendor:haproxy) and path:/api')).toMatchObject({
      vendors: ['nginx', 'haproxy'], path: '/api',
    })
  })

  it('rejects not', () => {
    expect(err('not vendor:nginx')).toMatch(/not.*isn't supported/)
  })

  it('rejects or across different fields', () => {
    expect(err('hostname:a or hostname:b')).toMatch(/or.*across.*hostname/)
  })

  it('rejects and repeated on the same facet field', () => {
    expect(err('vendor:nginx and vendor:haproxy')).toMatch(/set more than once/)
  })

  it('rejects a single field set twice', () => {
    expect(err('hostname:a and hostname:b')).toMatch(/only be set once/)
  })

  it('rejects an unknown field', () => {
    expect(err('color:blue')).toMatch(/unknown field/)
  })

  it('rejects a non-numeric port', () => {
    expect(err('port:abc')).toMatch(/must be a number/)
  })

  it('rejects a bare value with no field', () => {
    expect(err('payments.corp.example')).toMatch(/field:value shape/)
  })

  it('round-trips through stringify', () => {
    const q: RuleQuery = { hostname: 'payments.corp.example', path: '/api/v2/charge', scheme: '', port: '', classes: [], vendors: ['nginx', 'haproxy'] }
    expect(ok(stringifyRuleQuery(q))).toEqual(q)
  })
})
