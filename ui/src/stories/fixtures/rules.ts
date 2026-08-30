import { RulesResponseSchema } from '../../api/pb/nagipath/api/v1/rules_pb'
import { pb } from '../support/mockApi'

const rules = [
  { ordinal: 1, directive: 'listen', actionClass: 'listener', args: '443 ssl http2', scope: 'server', path: '/etc/nginx/conf.d/checkout.conf', byteStart: 24, fileId: 9102n, snapshotId: 8801n },
  { ordinal: 2, directive: 'ssl_certificate', actionClass: 'tls', args: '/etc/ssl/checkout.pem', scope: 'server', path: '/etc/nginx/conf.d/checkout.conf', byteStart: 128, fileId: 9102n, snapshotId: 8801n },
  { ordinal: 3, directive: 'proxy_set_header', actionClass: 'header', args: 'X-Request-Id $request_id', scope: 'location /api/v2/', path: '/etc/nginx/conf.d/checkout.conf', byteStart: 448, fileId: 9102n, snapshotId: 8801n },
  { ordinal: 4, directive: 'proxy_pass', actionClass: 'proxy', args: 'http://checkout_api_canary', scope: 'location /api/v2/', path: '/etc/nginx/conf.d/checkout.conf', byteStart: 384, fileId: 9102n, snapshotId: 8801n },
  { ordinal: 5, directive: 'add_header', actionClass: 'header', args: 'Strict-Transport-Security max-age=31536000', scope: 'http', inherited: true, path: '/etc/nginx/nginx.conf', byteStart: 96, fileId: 9101n, snapshotId: 8801n },
  { ordinal: 6, directive: 'proxy_pass', actionClass: 'proxy', args: 'http://checkout_app', scope: 'location /', shadowed: true, shadowedBy: 'location /api/v2/ (more specific prefix)', path: '/etc/nginx/conf.d/checkout.conf', byteStart: 704, fileId: 9102n, snapshotId: 8801n },
]

const result = {
  instId: 900n,
  instDisplayName: 'nginx (:443)',
  instNodeName: 'app-iad3-17',
  instVendor: 'nginx',
  nodeId: 41n,
  clusterName: 'app-iad3',
  listenerAddress: '0.0.0.0',
  listenerPort: 443,
  listenerTls: true,
  siteName: 'checkout.example.com',
  matchedBy: 'server_name exact',
  routePattern: '/api/v2/',
  precedence: 'prefix, longest match wins',
  rules,
}

export const rulesFixture = pb(RulesResponseSchema, {
  asked: true,
  scheme: 'https',
  hostname: 'checkout.example.com',
  path: '/api/v2/cart',
  port: 443,
  url: 'https://checkout.example.com/api/v2/cart',
  classes: ['listener', 'tls', 'proxy', 'header', 'rewrite', 'auth', 'cache'],
  vendors: ['nginx', 'haproxy', 'apache', 'envoy'],
  classesSelected: [],
  vendorsSelected: [],
  rules: 6,
  rulesUnfiltered: 6,
  nodes: 312,
  files: 2,
  page: 1,
  pages: 1,
  from: 1,
  to: 2,
  vendorFacets: [{ value: 'nginx', count: 308 }, { value: 'haproxy', count: 4 }],
  classFacets: [{ value: 'proxy', count: 2 }, { value: 'header', count: 2 }, { value: 'tls', count: 1 }, { value: 'listener', count: 1 }],
  groups: [
    { nodeName: 'app-iad3-01', hash: 'a1b2c3', count: 308, results: [{ ...result, instId: 800n, instNodeName: 'app-iad3-01', routePattern: '/api/v2/', rules }] },
    { nodeName: 'app-iad3-17', hash: 'd4e5f6', count: 4, results: [result] },
  ],
  silent: [
    { instId: 640n, instDisplayName: 'haproxy (:443)', instNodeName: 'app-sfo2-04', instVendor: 'haproxy', nodeId: 88n, clusterName: 'app-sfo2', listenerPort: 443, listenerTls: true, siteName: 'checkout.example.com', reason: 'no backend rule matches this path', degraded: true, undetermined: [{ hopOrdinal: 1, raw: 'use_backend %[req.hdr(host),lower,map_dom(/etc/haproxy/hosts.map)]', reason: 'map file not collected' }] },
  ],
})

export const unaskedRulesFixture = pb(RulesResponseSchema, {
  scheme: 'https',
  port: 443,
  classes: ['listener', 'tls', 'proxy', 'header', 'rewrite', 'auth', 'cache'],
  vendors: ['nginx', 'haproxy', 'apache', 'envoy'],
})

export const emptyRulesFixture = pb(RulesResponseSchema, {
  asked: true,
  empty: true,
  scheme: 'https',
  hostname: 'nothing.example.com',
  path: '/',
  port: 443,
  url: 'https://nothing.example.com/',
  classes: ['listener', 'tls', 'proxy', 'header'],
  vendors: ['nginx', 'haproxy'],
})
