import { create, toJson, type DescMessage, type MessageInitShape } from '@bufbuild/protobuf'

// pb() builds a fixture body the way the server does: a typed proto message
// marshalled with protojson, so a field the page reads but the .proto file
// does not have is a compile error here rather than an empty cell in the story.
export function pb<D extends DescMessage>(schema: D, init: MessageInitShape<D>): unknown {
  return toJson(schema, create(schema, init))
}

// Keys are API paths without the /api prefix, matched by longest prefix, so
// '/nodes' answers '/nodes?q=edge' and '/nodes/42' takes precedence over it.
export type ApiFixtures = Record<string, unknown>

let fixtures: ApiFixtures = {}
let missed: string[] = []

export function setApiFixtures(next: ApiFixtures) {
  fixtures = next
  missed = []
}

// Every GET a story fired that no fixture answered. The story smoke test reads
// this, so a fixture that stops matching its endpoint fails the test run instead
// of quietly rendering an error state nobody opens.
export function takeMissedFixtures(): string[] {
  const taken = missed
  missed = []
  return taken
}

function match(path: string): string | undefined {
  return Object.keys(fixtures)
    .filter((key) => path === key || path.startsWith(key))
    .sort((a, b) => b.length - a.length)[0]
}

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

// Storybook has no server behind it, so every /api call is answered from the
// story's own `parameters.api` fixtures. Anything else (fonts, static assets)
// falls through to the real fetch.
export function installApiMock() {
  const realFetch = window.fetch?.bind(window) ?? globalThis.fetch
  window.fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
    const { pathname, search } = new URL(url, window.location.origin)
    if (!pathname.startsWith('/api')) return realFetch(input, init)

    const path = pathname.slice('/api'.length)
    const key = match(path)
    if (key !== undefined) return json(fixtures[key])

    // A mutation with no fixture gets the {ok:true} envelope api.postAction
    // expects — a story for a form should not need a fixture per button.
    const method = (init?.method ?? (input instanceof Request ? input.method : 'GET')).toUpperCase()
    if (method !== 'GET' && method !== 'HEAD') return json({ ok: true })

    missed.push(`GET ${path}`)
    return json(
      {
        error: {
          code: 'not_found',
          message: `no story fixture for GET ${path}${search} — add it to this story's parameters.api`,
        },
      },
      404,
    )
  }
}
