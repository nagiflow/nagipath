import {
  ImportNodesResponseSchema,
  NodeDetailResponseSchema,
  NodesListResponseSchema,
  TestConnectionResponseSchema,
} from '../../api/pb/nagipath/api/v1/nodes_pb'
import { pb } from '../support/mockApi'

export const nodesListFixture = pb(NodesListResponseSchema, {
  total: 428,
  threshold: 3,
  pending: 3,
  quarantined: 1,
  neverCollected: 2,
  nodes: [
    { id: 12n, address: '10.4.12.1', sshPort: 22, displayName: 'app-iad3-01', osFamily: 'linux', enabled: true, instanceCount: 2, lastCollection: '2026-08-30T09:38:00Z', lastStatus: 'succeeded', vendor: 'nginx', version: '1.24.0', cluster: 'app-iad3', processCount: 2, listeners: '443 ssl, 80', lastCaptured: '2026-08-30T09:38:00Z' },
    { id: 41n, address: '10.4.12.17', sshPort: 22, displayName: 'app-iad3-17', osFamily: 'linux', enabled: true, instanceCount: 2, lastCollection: '2026-08-30T09:35:00Z', lastStatus: 'degraded', vendor: 'nginx', version: '1.24.0', cluster: 'app-iad3', processCount: 2, listeners: '443 ssl, 80', lastCaptured: '2026-08-30T09:35:00Z' },
    { id: 88n, address: '10.9.3.4', sshPort: 22, displayName: 'app-sfo2-04', osFamily: 'linux', enabled: true, instanceCount: 1, lastCollection: '2026-08-30T09:20:00Z', lastStatus: 'succeeded', vendor: 'haproxy', version: '2.8.5', cluster: 'app-sfo2', processCount: 1, listeners: '443 ssl', lastCaptured: '2026-08-30T09:20:00Z' },
    { id: 104n, address: '10.0.4.9', sshPort: 2202, displayName: 'lb-legacy-04', osFamily: 'linux', enabled: true, consecutiveFailures: 6, instanceCount: 1, pendingHostKeys: 1, lastCollection: '2026-08-30T09:40:00Z', lastStatus: 'failed', vendor: 'apache', version: '2.4.52', cluster: 'legacy', processCount: 1, listeners: '8443 ssl', lastCaptured: '2026-08-21T02:40:00Z' },
    { id: 210n, address: '10.7.1.9', sshPort: 22, displayName: 'edge-sfo2-02', osFamily: 'linux', enabled: false, instanceCount: 0, lastStatus: '', vendor: '', cluster: 'edge-sfo2', processCount: 0, listeners: '' },
  ],
})

export const emptyNodesListFixture = pb(NodesListResponseSchema, { query: 'nothing-matches-this', threshold: 3 })

const node = {
  id: 41n,
  address: '10.4.12.17',
  sshPort: 22,
  displayName: 'app-iad3-17',
  sshUsername: 'nagipath',
  credentialId: 2n,
  osFamily: 'linux',
  sudoAvailable: true,
  enabled: true,
  source: 'import',
  notes: 'canary node for the checkout API',
  firstSeenAt: '2026-05-02T10:00:00Z',
  lastCollection: '2026-08-30T09:35:00Z',
  lastStatus: 'degraded',
}

const instances = [
  { id: 900n, nodeId: 41n, clusterId: 2n, vendor: 'nginx', displayName: 'nginx (:443)', version: '1.24.0', mainConfigPath: '/etc/nginx/nginx.conf', nodeDisplayName: 'app-iad3-17', siteCount: 31, routeCount: 214, certCount: 6, lastCaptured: '2026-08-30T09:35:00Z', clusterName: 'app-iad3', parseState: 'parsed', state: 'drift' },
  { id: 901n, nodeId: 41n, clusterId: 2n, vendor: 'nginx', displayName: 'nginx (:80)', version: '1.24.0', mainConfigPath: '/etc/nginx/nginx.conf', nodeDisplayName: 'app-iad3-17', siteCount: 31, routeCount: 34, certCount: 0, lastCaptured: '2026-08-30T09:35:00Z', clusterName: 'app-iad3', parseState: 'parsed', state: 'conforming' },
]

// Matches nodeservice.go's GetNode exactly: base + "/" + tab + "?process=" +
// selected.id — Overview has no extra path segment, every other tab does.
const tabs = [
  { label: 'Overview', href: '/nodes/41?process=900', count: 0, on: true },
  { label: 'Sites', href: '/nodes/41/sites?process=900', count: 31, on: false },
  { label: 'Routes', href: '/nodes/41/routes?process=900', count: 214, on: false },
  { label: 'Upstreams', href: '/nodes/41/upstreams?process=900', count: 8, on: false },
  { label: 'Certificates', href: '/nodes/41/certificates?process=900', count: 6, on: false },
  { label: 'Config files', href: '/nodes/41/files?process=900', count: 14, on: false },
  { label: 'Drift', href: '/nodes/41/drift?process=900', count: 4, on: false },
]

const base = {
  node,
  instances,
  selected: instances[0],
  tabs,
  credentialName: 'iad3-collector',
  threshold: 3,
  stats: { sites: 31, routes: 214, upstreams: 8, certCount: 6, driftCount: 4 },
  credentials: [
    { id: 2n, name: 'iad3-collector', username: 'nagipath', authKind: 'ssh_key', fingerprint: 'SHA256:9mQ…v1c', createdAt: '2026-05-01T08:00:00Z' },
    { id: 3n, name: 'legacy-password', username: 'root', authKind: 'password', createdAt: '2026-06-11T08:00:00Z' },
  ],
  hostKeys: [
    { id: 55n, nodeId: 41n, algorithm: 'ssh-ed25519', fingerprint: 'SHA256:2Fq…8kZ', state: 'approved', firstSeenAt: '2026-05-02T10:00:00Z', decidedAt: '2026-05-02T10:04:00Z' },
  ],
}

// Overview (design/ screen 3c).
export const nodeDetailFixture = pb(NodeDetailResponseSchema, { ...base, tab: '' })

// Routes (5a) — the two-pane site/route list with the selected route's effect.
export const nodeRoutesFixture = pb(NodeDetailResponseSchema, {
  ...base,
  tab: 'routes',
  selectedSiteName: 'checkout.example.com',
  inst: {
    id: 900n,
    nodeName: 'app-iad3-17',
    vendor: 'nginx',
    displayName: 'nginx (:443)',
    clusterName: 'app-iad3',
    sites: [
      {
        id: 300n,
        primaryName: 'checkout.example.com',
        kind: 'server',
        names: ['checkout.example.com', 'checkout-int.example.com'],
        routes: [
          { id: 4001n, matchType: 'exact', pattern: '= /healthz', ordinal: 1, isTerminal: true, targetRaw: 'return 200' },
          { id: 4002n, matchType: 'prefix', pattern: '/api/v2/', ordinal: 2, isTerminal: true, upstreamId: 700n, targetRaw: 'proxy_pass http://checkout_api_canary' },
          { id: 4003n, matchType: 'prefix', pattern: '/static/', ordinal: 3, isTerminal: true, targetRaw: 'root /srv/checkout/static' },
          { id: 4004n, matchType: 'prefix', pattern: '/', ordinal: 4, isTerminal: true, upstreamId: 701n, targetRaw: 'proxy_pass http://checkout_app' },
        ],
      },
      { id: 301n, primaryName: 'www.example.com', kind: 'server', names: ['www.example.com'], routes: [{ id: 4100n, matchType: 'prefix', pattern: '/', ordinal: 1, isTerminal: true, upstreamId: 701n, targetRaw: 'proxy_pass http://www_app' }] },
    ],
    upstreams: [
      { id: 700n, name: 'checkout_api_canary', kind: 'http', balanceMethod: 'round_robin', members: [{ id: 1n, host: '10.4.12.90', port: 8080, scheme: 'http' }] },
      { id: 701n, name: 'checkout_app', kind: 'http', balanceMethod: 'least_conn', members: [{ id: 2n, host: '10.4.12.11', port: 8080, scheme: 'http' }, { id: 3n, host: '10.4.12.12', port: 8080, scheme: 'http', flags: 'backup' }] },
    ],
  },
  selectedRoute: { id: 4002n, matchType: 'prefix', pattern: '/api/v2/', ordinal: 2, isTerminal: true, upstreamId: 700n, targetRaw: 'proxy_pass http://checkout_api_canary' },
  routeEffect: {
    upstreamName: 'checkout_api_canary',
    memberCount: 2,
    balanceMethod: 'round_robin',
    members: [
      { id: 1n, host: '10.4.12.90', port: 8080, scheme: 'http' },
      { id: 4n, host: '10.4.12.91', port: 8080, scheme: 'http', flags: 'down' },
    ],
  },
})

// Upstreams (2o) — pool table with the open pool's members underneath.
export const nodeUpstreamsFixture = pb(NodeDetailResponseSchema, {
  ...base,
  tab: 'upstreams',
  upstreams: [
    { id: 700n, name: 'checkout_api_canary', kind: 'http', balanceMethod: 'round_robin', memberCount: 2, usedBy: 'checkout.example.com /api/v2/', resolution: 'static', state: 'verified' },
    { id: 701n, name: 'checkout_app', kind: 'http', balanceMethod: 'least_conn', memberCount: 8, usedBy: 'checkout.example.com /', resolution: 'static', state: 'verified' },
    { id: 702n, name: 'www_app', kind: 'http', balanceMethod: 'round_robin', memberCount: 6, usedBy: 'www.example.com /', resolution: 'dns', state: 'inferred' },
    { id: 703n, name: 'legacy_soap', kind: 'http', balanceMethod: 'ip_hash', memberCount: 0, usedBy: '—', resolution: 'unresolved', state: 'missing' },
  ],
  selectedPool: { id: 700n, name: 'checkout_api_canary', kind: 'http', balanceMethod: 'round_robin', memberCount: 2, usedBy: 'checkout.example.com /api/v2/', resolution: 'static', state: 'verified' },
  poolMembers: [
    { host: '10.4.12.90', port: 8080, weight: 1, role: 'primary', nodeName: 'app-iad3-17' },
    { host: '10.4.12.91', port: 8080, weight: 1, flags: 'down', role: 'primary', nodeName: 'app-iad3-17' },
  ],
})

// Certificates (2p).
export const nodeCertificatesFixture = pb(NodeDetailResponseSchema, {
  ...base,
  tab: 'certificates',
  certificates: [
    { subjectCn: 'checkout.example.com', siteName: 'checkout.example.com', port: 443, notAfter: '2026-09-08T00:00:00Z', fingerprint: 'SHA256:a1b…9f0', keyType: 'ECDSA P-256', chainLength: 2 },
    { subjectCn: '*.example.com', siteName: 'www.example.com', port: 443, notAfter: '2027-02-01T00:00:00Z', fingerprint: 'SHA256:c4d…12a', keyType: 'RSA 2048', chainLength: 3 },
  ],
})

// Config files (5b) — file list plus the selected file's body.
export const nodeFilesFixture = pb(NodeDetailResponseSchema, {
  ...base,
  tab: 'files',
  snapshot: { id: 8801n, capturedAt: '2026-08-30T09:35:00Z', degraded: true, degradedReason: '2 includes unreadable', fileCount: 14, bytesRaw: 184320n, parseState: 'parsed' },
  files: [
    { id: 9101n, kind: 'main', path: '/etc/nginx/nginx.conf', bytesRaw: 4096n },
    { id: 9102n, kind: 'include', path: '/etc/nginx/conf.d/checkout.conf', bytesRaw: 2048n },
    { id: 9103n, kind: 'include', path: '/etc/nginx/conf.d/www.conf', bytesRaw: 1536n },
    { id: 9104n, kind: 'include', path: '/etc/nginx/snippets/tls.conf', bytesRaw: 512n, truncated: true },
  ],
  selectedFile: { id: 9102n, kind: 'include', path: '/etc/nginx/conf.d/checkout.conf', bytesRaw: 2048n },
  lineStart: 12,
  byteStart: 384,
  fileBody: [
    'server {',
    '    listen 443 ssl http2;',
    '    server_name checkout.example.com checkout-int.example.com;',
    '',
    '    ssl_certificate     /etc/ssl/checkout.pem;',
    '    ssl_certificate_key /etc/ssl/checkout.key;',
    '',
    '    location = /healthz { return 200; }',
    '',
    '    location /api/v2/ {',
    '        proxy_pass http://checkout_api_canary;',
    '        proxy_set_header X-Request-Id $request_id;',
    '    }',
    '',
    '    location /static/ { root /srv/checkout/static; }',
    '    location / { proxy_pass http://checkout_app; }',
    '}',
  ].join('\n'),
})

// Drift tab of the node (the per-node view of design/ screen 6b's findings).
export const nodeDriftFixture = pb(NodeDetailResponseSchema, {
  ...base,
  tab: 'drift',
  hasDrift: true,
  driftByObject: { route: 3, upstream: 1 },
  driftRuns: [
    { id: 660n, instanceId: 900n, baselineKind: 'golden_peer', baselineLabel: 'app-iad3-01', computedAt: '2026-08-30T09:36:00Z', findingCount: 4, instanceName: 'nginx (:443)', nodeName: 'app-iad3-17', vendor: 'nginx', parserVersion: 7, ignoredCount: 1 },
  ],
  driftFindings: [
    { id: 7701n, objectKind: 'route', naturalKey: 'checkout.example.com /api/v2/', change: 'modified', field: 'upstream', baselineText: 'checkout_api', subjectText: 'checkout_api_canary', actionClass: 'proxy', provenance: { path: '/etc/nginx/conf.d/checkout.conf', snapshotId: 8801n, fileId: 9102n, byteStart: 384n, link: '/snapshots/8801/file/9102?b=384' } },
    { id: 7702n, objectKind: 'upstream', naturalKey: 'checkout_api_canary', change: 'added', field: '', subjectText: '2 members', actionClass: 'upstream', provenance: { path: '/etc/nginx/conf.d/checkout.conf', snapshotId: 8801n, fileId: 9102n, byteStart: 1024n, link: '/snapshots/8801/file/9102?b=1024' } },
  ],
})

export const importNodesFixture = pb(ImportNodesResponseSchema, {
  added: [
    { id: 431n, displayName: 'lb01.example.com', address: 'lb01.example.com', sshPort: 22 },
    { id: 432n, displayName: 'web01', address: '10.90.4.2', sshPort: 22 },
    { id: 433n, displayName: 'db02.example.com', address: 'db02.example.com', sshPort: 22 },
    { id: 434n, displayName: 'edge-haproxy-03', address: 'edge-haproxy-03', sshPort: 22 },
  ],
  refused: [
    'line 6: "10.90.4.0/24" — a range is not a host',
    'line 9: "db01.example.com" — already in inventory',
  ],
})

// One /nodes/{id}/test fixture per node in importNodesFixture.added above, so
// step 2 of the Import inventory story shows every connection-test outcome at
// once: connected, blocked on an unapproved host key, and failed.
export const importTestConnectedFixture = pb(TestConnectionResponseSchema, {
  status: 'connected', latencyMs: 41, osFamily: 'linux',
})
export const importTestHostKeyPendingFixture = pb(TestConnectionResponseSchema, {
  status: 'host_key_pending',
  pendingKey: { id: 901n, nodeId: 432n, algorithm: 'ed25519', fingerprint: 'SHA256:pR7g…4Nc', state: 'pending', firstSeenAt: '2026-08-31T04:31:00Z' },
})
export const importTestFailedFixture = pb(TestConnectionResponseSchema, {
  status: 'failed', error: 'dial tcp 10.9.4.4:22: connect: connection refused',
})
