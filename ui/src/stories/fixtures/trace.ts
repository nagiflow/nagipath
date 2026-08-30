import { ProbeDetailResponseSchema, ProbeHistoryResponseSchema } from '../../api/pb/nagipath/api/v1/probe_pb'
import { TraceResponseSchema } from '../../api/pb/nagipath/api/v1/trace_pb'
import { pb } from '../support/mockApi'

const recent = [
  { id: 9001n, scheme: 'https', hostname: 'checkout.example.com', path: '/api/v2/cart', port: 443, computedAt: '2026-08-30T09:41:00Z', hopCount: 3, confidence: 'verified', terminalReason: 'upstream' },
  { id: 9000n, scheme: 'https', hostname: 'www.example.com', path: '/', port: 443, computedAt: '2026-08-30T09:12:00Z', hopCount: 2, confidence: 'inferred', terminalReason: 'external_hop' },
]

// design/ screen 2a: three hops, the last one leaving the fleet.
export const traceFixture = pb(TraceResponseSchema, {
  asked: true,
  scheme: 'https',
  hostname: 'checkout.example.com',
  path: '/api/v2/cart',
  port: 443,
  method: 'GET',
  url: 'https://checkout.example.com/api/v2/cart',
  traceId: 9001n,
  recent,
  firedCount: 14,
  scopeCount: 6,
  collapsedCount: 308,
  verifiedCount: 2,
  inferredCount: 1,
  degradedCount: 0,
  externalCount: 1,
  terminalReason: 'upstream returned the response',
  probesCount: 4,
  hops: [
    { ordinal: 0, label: '0', instId: 120n, instNodeName: 'edge-iad3-01', instDisplayName: 'nginx (:443)', instVendor: 'nginx', listenerPort: 443, siteKind: 'server', siteName: 'checkout.example.com', routeKind: 'prefix', routePattern: '/api/', confidence: 'verified', inboundPath: '/api/v2/cart', effectivePath: '/api/v2/cart', clusterName: 'edge-iad3', level: 0, firedRules: [{ ruleId: 3001n, directive: 'proxy_pass', args: 'http://app_iad3', actionClass: 'proxy' }] },
    { ordinal: 1, label: '1-a', instId: 900n, instNodeName: 'app-iad3-17', instDisplayName: 'nginx (:443)', instVendor: 'nginx', listenerPort: 443, siteKind: 'server', siteName: 'checkout.example.com', routeKind: 'prefix', routePattern: '/api/v2/', confidence: 'verified', inboundPath: '/api/v2/cart', effectivePath: '/cart', clusterName: 'app-iad3', arrivedFrom: 0, level: 1, firedRules: [{ ruleId: 4002n, directive: 'proxy_pass', args: 'http://checkout_api_canary', actionClass: 'proxy' }, { ruleId: 4005n, directive: 'proxy_set_header', args: 'X-Request-Id $request_id', actionClass: 'header' }] },
    { ordinal: 2, label: '2', isExternal: true, externalTarget: '10.4.12.90:8080', externalReason: 'upstream member is not a collected node', confidence: 'external_hop', inboundPath: '/cart', arrivedFrom: 1, level: 2 },
  ],
  entryCandidates: [
    { instId: 120n, nodeId: 12n, instDisplayName: 'nginx (:443) on edge-iad3-01', instNodeAddress: '10.7.1.1', listenerPort: 443, selected: true, reason: 'server_name exact match' },
    { instId: 121n, nodeId: 13n, instDisplayName: 'nginx (:443) on edge-iad3-02', instNodeAddress: '10.7.1.2', listenerPort: 443, reason: 'identical config, collapsed' },
  ],
  lastProbe: {
    outcome: 'verified',
    method: 'GET',
    requestedAt: '2026-08-30T09:42:00Z',
    probed: [
      { ordinal: 0, label: '0', evidence: 'access log line matched correlation token', before: 'inferred', after: 'verified' },
      { ordinal: 1, label: '1-a', evidence: 'access log line matched correlation token', before: 'inferred', after: 'verified' },
    ],
  },
  prov: {
    ruleId: 4002n,
    instance: 'nginx (:443) on app-iad3-17',
    object: 'location /api/v2/',
    rule: 'proxy_pass http://checkout_api_canary',
    path: '/etc/nginx/conf.d/checkout.conf',
    digest: 'sha256:9f0…a1b',
    snapshotId: 8801n,
    fileId: 9102n,
    byteStart: 384,
    line: 11,
    lines: [
      { n: 9, text: '    location /api/v2/ {' },
      { n: 10, text: '        proxy_pass http://checkout_api_canary;', on: true },
      { n: 11, text: '        proxy_set_header X-Request-Id $request_id;' },
      { n: 12, text: '    }' },
    ],
  },
  provPicked: true,
})

export const unaskedTraceFixture = pb(TraceResponseSchema, { scheme: 'https', port: 443, method: 'GET', recent })

export const emptyTraceFixture = pb(TraceResponseSchema, { empty: true, scheme: 'https', port: 443, method: 'GET' })

// A probe run mid-flight — the live step list design/ 2a's "Probe" panel polls.
export const probeRunFixture = pb(TraceResponseSchema, {
  asked: true,
  scheme: 'https',
  hostname: 'checkout.example.com',
  path: '/api/v2/cart',
  port: 443,
  method: 'GET',
  url: 'https://checkout.example.com/api/v2/cart',
  traceId: 9001n,
  recent,
  run: {
    id: 5001n,
    url: 'https://checkout.example.com/api/v2/cart',
    method: 'GET',
    done: false,
    steps: [
      { ordinal: 0, text: 'request sent with correlation token np-7f3c', instance: 'nginx (:443) on edge-iad3-01', state: 'verified' },
      { ordinal: 1, text: 'reading access log /var/log/nginx/access.log', instance: 'nginx (:443) on app-iad3-17', state: 'running' },
    ],
    state: { 0: 'verified', 1: 'running' },
  },
})

export const probeHistoryFixture = pb(ProbeHistoryResponseSchema, {
  url: 'https://checkout.example.com/api/v2/cart',
  range: '7d',
  outcome: 'all',
  total: 46,
  actorCount: 3,
  from: 1,
  to: 3,
  hasMore: true,
  nextCursor: '4990',
  actors: ['awong', 'rlee', 'api:ci-deploy'],
  probes: [
    { probeId: 5001n, token: 'np-7f3c', method: 'GET', url: 'https://checkout.example.com/api/v2/cart', status: 200, result: 'verified', actorLabel: 'awong', originHost: '10.7.1.1', requestedAt: '2026-08-30T09:42:00Z', evidence: 2, verified: 2, changes: ['hop 1-a inferred → verified'], raised: 1 },
    { probeId: 4998n, token: 'np-2b91', method: 'GET', url: 'https://checkout.example.com/api/v2/cart', status: 502, result: 'failed', error: 'upstream returned 502', actorLabel: 'api:ci-deploy', originHost: '10.7.1.1', requestedAt: '2026-08-29T22:10:00Z', evidence: 1, verified: 0 },
    { probeId: 4990n, token: 'np-55da', method: 'HEAD', url: 'https://www.example.com/', status: 301, result: 'partial', actorLabel: 'rlee', originHost: '10.7.1.2', requestedAt: '2026-08-29T14:02:00Z', evidence: 1, verified: 1, changes: ['hop 0 inferred → verified'] },
  ],
  selected: {
    probeId: 5001n,
    token: 'np-7f3c',
    method: 'GET',
    url: 'https://checkout.example.com/api/v2/cart',
    status: 200,
    statusText: '200 OK',
    durationMs: 214n,
    requestedAt: '2026-08-30T09:42:00Z',
    actorLabel: 'awong',
    originHost: '10.7.1.1',
    outcome: 'verified',
    redirectCount: 0,
    logReads: '2 of 2 hops log-capable',
    changeCount: 1,
    evidence: [
      { hopOrdinal: 0, instance: 'nginx (:443)', node: 'edge-iad3-01', kind: 'access_log', logPath: '/var/log/nginx/access.log', raw: '10.7.1.1 - - [30/Aug/2026:09:42:00] "GET /api/v2/cart HTTP/1.1" 200 np-7f3c', grants: 'verified', observedAt: '2026-08-30T09:42:00Z', prior: 'inferred' },
      { hopOrdinal: 1, instance: 'nginx (:443)', node: 'app-iad3-17', kind: 'access_log', logPath: '/var/log/nginx/access.log', raw: '10.7.1.1 - - [30/Aug/2026:09:42:00] "GET /cart HTTP/1.1" 200 np-7f3c', grants: 'verified', observedAt: '2026-08-30T09:42:00Z', prior: 'inferred' },
    ],
    changes: [{ host: 'app-iad3-17', prior: 'inferred', grants: 'verified', kind: 'access_log', logPath: '/var/log/nginx/access.log', to: 'verified' }],
  },
})

export const probeDetailFixture = pb(ProbeDetailResponseSchema, {
  stateChanges: 2,
  probe: {
    probeId: 5001n,
    traceId: 9001n,
    actorUsername: 'awong',
    method: 'GET',
    url: 'https://checkout.example.com/api/v2/cart',
    maxRedirects: 5,
    correlationToken: 'np-7f3c',
    originHost: '10.7.1.1',
    requestedAt: '2026-08-30T09:42:00Z',
    status: 200,
    durationMs: 214n,
    redirectCount: 0,
    serverHeader: 'nginx',
    viaHeader: '1.1 edge-iad3-01',
    result: 'verified',
    logCapableCount: 2,
    verifiedCount: 2,
    stillInferred: 1,
    retentionDays: 30,
  },
  hops: [
    { hopOrdinal: 0, nodeName: 'edge-iad3-01', vendor: 'nginx', evidence: 'access log line matched np-7f3c', before: 'inferred', after: 'verified' },
    { hopOrdinal: 1, nodeName: 'app-iad3-17', vendor: 'nginx', evidence: 'access log line matched np-7f3c', before: 'inferred', after: 'verified' },
  ],
  gaps: [
    { hopOrdinal: 2, nodeName: '10.4.12.90', vendor: '', evidence: '', hasGap: true, gapVendor: 'unknown', gapNote: 'upstream member is not a collected node — nothing to read a log from', gapDirective: '' },
  ],
  logLines: [
    { nodeName: 'edge-iad3-01', logPath: '/var/log/nginx/access.log', rawLine: '10.7.1.1 - - [30/Aug/2026:09:42:00] "GET /api/v2/cart HTTP/1.1" 200 np-7f3c' },
    { nodeName: 'app-iad3-17', logPath: '/var/log/nginx/access.log', rawLine: '10.7.1.1 - - [30/Aug/2026:09:42:00] "GET /cart HTTP/1.1" 200 np-7f3c' },
  ],
})
