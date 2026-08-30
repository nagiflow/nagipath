// design/'s route tables (screens 7a, 5a) never print a parser's match_type
// enum — they print the plain-English sentence a reviewer can read out loud:
// "starts with /api/", "matches ^/api/v[0-9]+/", "everything else". These are
// the two phrasings that projection needs; the enum values are the CHECK list
// in internal/store/migrations/001_init.sql's route.match_type.
const matchWords: Record<string, string> = {
  exact: 'exactly',
  prefix: 'starts with',
  prefix_no_regex: 'starts with',
  regex: 'matches',
  regex_ci: 'matches (any case)',
  named: 'named location',
  directory: 'directory',
  directory_match: 'directory matching',
  location: 'starts with',
  location_match: 'matches',
  files: 'file named',
  files_match: 'file matching',
  haproxy_acl_use_backend: 'when',
  haproxy_default_backend: 'everything else',
}

export function matchPhrase(matchType: string, pattern: string): string {
  // `location /` (and haproxy's default_backend) is the catch-all every other
  // route falls through to, which reads better as a sentence than "starts with /".
  if (matchType === 'haproxy_default_backend' || pattern === '' || pattern === '/') return 'everything else'
  const word = matchWords[matchType]
  return word ? `${word} ${pattern}` : pattern
}

// What the route does with the request: an upstream means it forwards, a
// filesystem target means it serves files, a URL means a redirect. Anything
// else is the raw target the parser recorded.
export function actionPhrase(upstream: string, target: string): string {
  if (upstream) return 'forwards'
  if (/^https?:\/\//.test(target)) return 'redirects'
  if (target.startsWith('/')) return 'serves files'
  if (target) return 'answers here'
  return '—'
}
