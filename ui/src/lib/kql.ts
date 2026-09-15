// A KQL-like grammar for the Rule lookup query bar: field:value terms
// combined with and/or/not and parens, e.g.
//   hostname:payments.corp.example and path:"/api/v2/charge"
//
// GetRules (rules.proto) only ever answers "rules for one target request"
// (one hostname + one path + one scheme + one port, always ANDed) plus
// vendor/class as inclusive multi-select facets (OR within the same field,
// same as ticking multiple checkboxes). compile() rejects anything outside
// that shape — not, or across different fields, and repeated on the same
// facet field — with a specific message, rather than silently dropping part
// of the query or reinterpreting "and" as "or".

export interface RuleQuery {
  hostname: string
  path: string
  scheme: string
  port: string
  classes: string[]
  vendors: string[]
}

export type ParseResult = { query: RuleQuery } | { error: string }

const SINGLE_FIELDS = ['hostname', 'path', 'scheme', 'port'] as const
const FACET_FIELDS = ['vendor', 'class'] as const
type SingleField = (typeof SINGLE_FIELDS)[number]
type FacetField = (typeof FACET_FIELDS)[number]
const ALL_FIELDS = [...SINGLE_FIELDS, ...FACET_FIELDS]

function isSingleField(f: string): f is SingleField {
  return (SINGLE_FIELDS as readonly string[]).includes(f)
}
function isFacetField(f: string): f is FacetField {
  return (FACET_FIELDS as readonly string[]).includes(f)
}

// ---------------------------------------------------------------- lexer

type Token =
  | { kind: 'word'; value: string }
  | { kind: 'string'; value: string }
  | { kind: 'colon' }
  | { kind: 'lparen' }
  | { kind: 'rparen' }
  | { kind: 'eof' }

function tokenize(input: string): Token[] {
  const tokens: Token[] = []
  let i = 0
  while (i < input.length) {
    const c = input[i]
    if (/\s/.test(c)) { i++; continue }
    if (c === '(') { tokens.push({ kind: 'lparen' }); i++; continue }
    if (c === ')') { tokens.push({ kind: 'rparen' }); i++; continue }
    if (c === ':') { tokens.push({ kind: 'colon' }); i++; continue }
    if (c === '"') {
      let j = i + 1
      let out = ''
      while (j < input.length && input[j] !== '"') {
        if (input[j] === '\\' && j + 1 < input.length) { out += input[j + 1]; j += 2 } else { out += input[j]; j++ }
      }
      if (j >= input.length) throw new Error('a quoted value is missing its closing "')
      tokens.push({ kind: 'string', value: out })
      i = j + 1
      continue
    }
    let j = i
    while (j < input.length && !/[\s():]/.test(input[j])) j++
    tokens.push({ kind: 'word', value: input.slice(i, j) })
    i = j
  }
  tokens.push({ kind: 'eof' })
  return tokens
}

// -------------------------------------------------------------------- AST

type Node =
  | { type: 'and'; items: Node[] }
  | { type: 'or'; items: Node[] }
  | { type: 'not'; item: Node }
  | { type: 'term'; field: string; value: string }

// Precedence, loosest to tightest: or, and, not — the usual KQL/Lucene order.
class Parser {
  private pos = 0
  private tokens: Token[]
  constructor(tokens: Token[]) { this.tokens = tokens }
  private peek(): Token { return this.tokens[this.pos] }
  private advance(): Token { return this.tokens[this.pos++] }
  private isKeyword(t: Token, w: string): boolean { return t.kind === 'word' && t.value.toLowerCase() === w }

  parse(): Node {
    const node = this.parseOr()
    if (this.peek().kind !== 'eof') throw new Error('unexpected text after the query — check for a missing "and"/"or"')
    return node
  }
  private parseOr(): Node {
    const items = [this.parseAnd()]
    while (this.isKeyword(this.peek(), 'or')) { this.advance(); items.push(this.parseAnd()) }
    return items.length > 1 ? { type: 'or', items } : items[0]
  }
  private parseAnd(): Node {
    const items = [this.parseNot()]
    while (this.isKeyword(this.peek(), 'and')) { this.advance(); items.push(this.parseNot()) }
    return items.length > 1 ? { type: 'and', items } : items[0]
  }
  private parseNot(): Node {
    if (this.isKeyword(this.peek(), 'not')) { this.advance(); return { type: 'not', item: this.parseNot() } }
    return this.parsePrimary()
  }
  private parsePrimary(): Node {
    const t = this.peek()
    if (t.kind === 'lparen') {
      this.advance()
      const node = this.parseOr()
      if (this.peek().kind !== 'rparen') throw new Error('missing a closing ")"')
      this.advance()
      return node
    }
    if (t.kind === 'word') {
      this.advance()
      const field = t.value
      if (this.peek().kind !== 'colon') {
        throw new Error(`every term needs a field:value shape, e.g. hostname:payments.corp.example — "${field}" has no ":"`)
      }
      this.advance()
      const valTok = this.advance()
      if (valTok.kind !== 'word' && valTok.kind !== 'string') throw new Error(`expected a value after "${field}:"`)
      return { type: 'term', field: field.toLowerCase(), value: valTok.value }
    }
    throw new Error('expected a field:value term, "(", or "not"')
  }
}

// ---------------------------------------------------------------- compile

interface Acc {
  single: Partial<Record<SingleField, string>>
  facets: Partial<Record<FacetField, string[]>>
  touched: Set<string>
}

function addSingleTerm(field: SingleField, value: string, acc: Acc): string | undefined {
  if (acc.touched.has(field)) return `"${field}" can only be set once`
  if (field === 'port' && !/^\d+$/.test(value)) return `"port" must be a number, got "${value}"`
  acc.touched.add(field)
  acc.single[field] = value
  return undefined
}

function addFacetTerm(field: FacetField, value: string, acc: Acc): string | undefined {
  if (acc.touched.has(field)) {
    return `"${field}" is set more than once with "and" — combine multiple ${field} values with "or" instead, like ${field}:a or ${field}:b`
  }
  acc.touched.add(field)
  acc.facets[field] = [value]
  return undefined
}

function addFacetOr(node: { type: 'or'; items: Node[] }, acc: Acc): string | undefined {
  let field: FacetField | undefined
  const values: string[] = []
  for (const item of node.items) {
    if (item.type !== 'term') return '"or" only works between repeated vendor/class values, like vendor:nginx or vendor:haproxy'
    if (isSingleField(item.field)) {
      return `"or" across "${item.field}" isn't supported — rule lookup always answers for one ${item.field}; combine different fields with "and" instead`
    }
    if (!isFacetField(item.field)) return `unknown field "${item.field}" — try ${ALL_FIELDS.join(', ')}`
    if (field === undefined) field = item.field
    else if (field !== item.field) return `"or" can't mix different fields ("${field}" and "${item.field}") — combine same-field values with "or", different fields with "and"`
    values.push(item.value)
  }
  if (!field) return 'an "or" group needs at least one term'
  if (acc.touched.has(field)) return `"${field}" is set more than once — combine all its values in one "or" group`
  acc.touched.add(field)
  acc.facets[field] = values
  return undefined
}

// walkAnd handles the top level and any nested "and" (parens flatten into it):
// each item is either a plain term, or an "or" group over one facet field.
function walkAnd(node: Node, acc: Acc): string | undefined {
  if (node.type === 'and') {
    for (const item of node.items) {
      const err = walkAndItem(item, acc)
      if (err) return err
    }
    return undefined
  }
  return walkAndItem(node, acc)
}

function walkAndItem(node: Node, acc: Acc): string | undefined {
  switch (node.type) {
    case 'and': return walkAnd(node, acc)
    case 'or': return addFacetOr(node, acc)
    case 'not': return '"not" isn\'t supported — rule lookup can only look up a value, not exclude one'
    case 'term':
      if (isSingleField(node.field)) return addSingleTerm(node.field, node.value, acc)
      if (isFacetField(node.field)) return addFacetTerm(node.field, node.value, acc)
      return `unknown field "${node.field}" — try ${ALL_FIELDS.join(', ')}`
  }
}

function compile(node: Node): ParseResult {
  const acc: Acc = { single: {}, facets: {}, touched: new Set() }
  const err = walkAnd(node, acc)
  if (err) return { error: err }
  return {
    query: {
      hostname: acc.single.hostname ?? '',
      path: acc.single.path ?? '',
      scheme: acc.single.scheme ?? '',
      port: acc.single.port ?? '',
      vendors: acc.facets.vendor ?? [],
      classes: acc.facets.class ?? [],
    },
  }
}

// -------------------------------------------------------------------- API

const EMPTY_QUERY: RuleQuery = { hostname: '', path: '', scheme: '', port: '', classes: [], vendors: [] }

export function parseRuleQuery(input: string): ParseResult {
  const trimmed = input.trim()
  if (!trimmed) return { query: EMPTY_QUERY }
  try {
    return compile(new Parser(tokenize(trimmed)).parse())
  } catch (e) {
    return { error: e instanceof Error ? e.message : String(e) }
  }
}

function quote(v: string): string {
  return v === '' || /[\s():"]/.test(v) ? `"${v.replace(/\\/g, '\\\\').replace(/"/g, '\\"')}"` : v
}

export function stringifyRuleQuery(q: RuleQuery): string {
  const parts: string[] = []
  if (q.hostname) parts.push(`hostname:${quote(q.hostname)}`)
  if (q.path) parts.push(`path:${quote(q.path)}`)
  if (q.scheme) parts.push(`scheme:${quote(q.scheme)}`)
  if (q.port) parts.push(`port:${quote(q.port)}`)
  for (const field of FACET_FIELDS) {
    const values = field === 'vendor' ? q.vendors : q.classes
    if (values.length === 0) continue
    const group = values.map((v) => `${field}:${quote(v)}`).join(' or ')
    parts.push(values.length > 1 ? `(${group})` : group)
  }
  return parts.join(' and ')
}
