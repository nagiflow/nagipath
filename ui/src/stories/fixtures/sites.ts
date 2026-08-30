import { SiteDetailResponseSchema, SitesListResponseSchema } from '../../api/pb/nagipath/api/v1/sites_pb'
import { pb } from '../support/mockApi'

const rows = [
  { name: 'www.example.com', aliasCount: 2, aliases: ['example.com', 'www2.example.com'], nodes: 312, listenerSummary: '0.0.0.0:443 ssl http2', certSubject: 'www.example.com', routes: 24, variants: 1, state: 'conforming', stateReason: '' },
  { name: 'checkout.example.com', aliasCount: 0, aliases: [], nodes: 312, listenerSummary: '0.0.0.0:443 ssl', certSubject: 'checkout.example.com', routes: 31, variants: 3, state: 'drift', stateReason: '3 config variants across 312 nodes' },
  { name: 'api.example.com', aliasCount: 1, aliases: ['api-internal.example.com'], nodes: 74, listenerSummary: '0.0.0.0:443 ssl http2', certSubject: '*.example.com', routes: 58, variants: 1, state: 'conforming', stateReason: '' },
  { name: 'static.cdn.example.net', aliasCount: 0, aliases: [], nodes: 18, listenerSummary: '0.0.0.0:80', certSubject: '', routes: 6, variants: 1, state: 'missing', stateReason: 'plaintext listener only' },
  { name: 'legacy-admin.example.com', aliasCount: 0, aliases: [], nodes: 24, listenerSummary: '10.0.4.9:8443 ssl', certSubject: 'legacy-admin.example.com', routes: 9, variants: 2, state: 'expiring', stateReason: 'certificate expires in 22 days' },
]

const stats = { hostnames: 1129, nodes: 428, variantHosts: 46, tlsTerminated: 1041, plaintext: 88, expiringCerts: 41, expiringBindings: 63 }

export const sitesListFixture = pb(SitesListResponseSchema, { rows, total: 1129, stats })

// The selected row's expansion (design/ screen 3a's ▾ detail): the same
// response with `sel` set and the per-variant route lists filled in.
export const sitesListSelectedFixture = pb(SitesListResponseSchema, {
  rows,
  total: 1129,
  stats,
  sel: 'checkout.example.com',
  variants: [
    {
      key: 'a1b2c3',
      nodes: 308,
      nodeNames: ['app-iad3-01', 'app-iad3-02', 'app-iad3-03', 'app-iad3-04'],
      routes: [
        { pattern: '/', action: 'proxy_pass', upstream: 'checkout_app' },
        { pattern: '/api/v2/', action: 'proxy_pass', upstream: 'checkout_api' },
        { pattern: '/static/', action: 'root', target: '/srv/checkout/static' },
        { pattern: '= /healthz', action: 'return', target: '200' },
      ],
    },
    {
      key: 'd4e5f6',
      nodes: 4,
      nodeNames: ['app-iad3-17', 'app-iad3-18', 'app-iad3-19', 'app-iad3-20'],
      routes: [
        { pattern: '/', action: 'proxy_pass', upstream: 'checkout_app' },
        { pattern: '/api/v2/', action: 'proxy_pass', upstream: 'checkout_api_canary' },
        { pattern: '/static/', action: 'root', target: '/srv/checkout/static' },
      ],
    },
  ],
})

export const emptySitesListFixture = pb(SitesListResponseSchema, { query: 'nothing-matches-this', stats: {} })

const tabs = [
  { label: 'Overview', href: '/sites/checkout.example.com', count: 0, on: true },
  { label: 'Nodes', href: '/sites/checkout.example.com?tab=nodes', count: 312, on: false },
  { label: 'Upstreams', href: '/sites/checkout.example.com?tab=upstreams', count: 3, on: false },
  { label: 'Certificates', href: '/sites/checkout.example.com?tab=certs', count: 2, on: false },
]

const variantOpts = [
  { key: '', label: 'All variants', on: true },
  { key: 'a1b2c3', label: 'Variant a1b2c3 · 308 nodes', on: false },
  { key: 'd4e5f6', label: 'Variant d4e5f6 · 4 nodes', on: false },
]

export const siteDetailFixture = pb(SiteDetailResponseSchema, {
  name: 'checkout.example.com',
  tab: 'overview',
  tabs,
  variantOpts,
  overview: {
    name: 'checkout.example.com',
    aliases: ['checkout-eu.example.com'],
    variants: 3,
    variantKey: 'a1b2c3',
    nodes: 312,
    clusters: [{ name: 'app-iad3', nodes: 238 }, { name: 'app-sfo2', nodes: 74 }],
    listenerSummary: '0.0.0.0:443 ssl http2',
    listenerFlags: 'ssl http2 reuseport',
    certSubject: 'checkout.example.com',
    certIssuer: 'CN=Example Internal CA G2',
    certExpiry: '2026-09-08T00:00:00Z',
    certExpiryDays: 9,
    certBindings: 12,
    certUncovered: ['checkout-eu.example.com'],
    upstreams: [
      { name: 'checkout_app', members: 8, variant: 'a1b2c3' },
      { name: 'checkout_api', members: 6, variant: 'a1b2c3' },
      { name: 'checkout_api_canary', members: 2, variant: 'd4e5f6' },
    ],
    routes: [
      { ordinal: 1, pattern: '= /healthz', matchType: 'exact', action: 'return', target: '200', variant: 'a1b2c3' },
      { ordinal: 2, pattern: '/api/v2/', matchType: 'prefix', action: 'proxy_pass', target: 'checkout_api', alsoDoes: 'sets X-Request-Id, strips /api/v2', variant: 'a1b2c3' },
      { ordinal: 3, pattern: '/static/', matchType: 'prefix', action: 'root', target: '/srv/checkout/static', alsoDoes: 'caches 7d', variant: 'a1b2c3' },
      { ordinal: 4, pattern: '/', matchType: 'prefix', action: 'proxy_pass', target: 'checkout_app', variant: 'a1b2c3' },
    ],
    stats: { nodes: 312, routes: 31, variants: 3, upstreams: 3, certDays: 9, certSubject: 'checkout.example.com' },
  },
  nodes: [
    { nodeId: 41n, nodeName: 'app-iad3-17', cluster: 'app-iad3', variant: 'd4e5f6', listener: '0.0.0.0:443 ssl', certificate: 'checkout.example.com', lastColl: '2026-08-30T09:35:00Z', state: 'drift', stateReason: 'canary upstream' },
    { nodeId: 12n, nodeName: 'app-iad3-01', cluster: 'app-iad3', variant: 'a1b2c3', listener: '0.0.0.0:443 ssl', certificate: 'checkout.example.com', lastColl: '2026-08-30T09:38:00Z', state: 'conforming' },
    { nodeId: 88n, nodeName: 'app-sfo2-04', cluster: 'app-sfo2', variant: 'a1b2c3', listener: '0.0.0.0:443 ssl', certificate: 'checkout.example.com', lastColl: '2026-08-30T09:20:00Z', state: 'conforming' },
  ],
  upstreams: [
    { upstream: 'checkout_app', host: '10.4.12.11', port: 8080, scheme: 'http', weight: 1, flags: '', nodeName: 'app-iad3-01' },
    { upstream: 'checkout_app', host: '10.4.12.12', port: 8080, scheme: 'http', weight: 1, flags: 'backup', nodeName: 'app-iad3-01' },
    { upstream: 'checkout_api_canary', host: '10.4.12.90', port: 8080, scheme: 'http', weight: 1, flags: '', nodeName: 'app-iad3-17' },
  ],
  certs: [
    { subject: 'checkout.example.com', sans: ['checkout.example.com', 'checkout-int.example.com'], issuer: 'CN=Example Internal CA G2', notAfter: '2026-09-08T00:00:00Z', expiryDays: 9, bindings: 12, uncovered: ['checkout-eu.example.com'] },
    { subject: '*.example.com', sans: ['*.example.com', 'example.com'], issuer: 'CN=Example Internal CA G2', notAfter: '2027-02-01T00:00:00Z', expiryDays: 155, bindings: 4, uncovered: [] },
  ],
})
