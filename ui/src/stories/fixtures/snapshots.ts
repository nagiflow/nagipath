import { SnapshotFileResponseSchema, SnapshotsListResponseSchema } from '../../api/pb/nagipath/api/v1/snapshots_pb'
import { pb } from '../support/mockApi'

export const snapshotsListFixture = pb(SnapshotsListResponseSchema, {
  range: '7d',
  changes: 'all',
  trigger: 'all',
  total: 18422,
  filtered: 5,
  changed: 2,
  degraded: 1,
  totalBytes: 4_294_967_296n,
  from: 1,
  to: 5,
  hasMore: true,
  nextCursor: '8790',
  retentionDays: 90,
  list: [
    { id: 8801n, instanceId: 900n, instance: 'nginx (:443)', node: 'app-iad3-17', cluster: 'app-iad3', capturedAt: '2026-08-30T09:35:00Z', trigger: 'schedule', changed: true, bytesRaw: 184320n, state: 'degraded' },
    { id: 8790n, instanceId: 800n, instance: 'nginx (:443)', node: 'app-iad3-01', cluster: 'app-iad3', capturedAt: '2026-08-30T09:38:00Z', trigger: 'manual', changed: false, bytesRaw: 182272n, state: 'parsed' },
    { id: 8712n, instanceId: 640n, instance: 'haproxy (:443)', node: 'app-sfo2-04', cluster: 'app-sfo2', capturedAt: '2026-08-30T09:20:00Z', trigger: 'schedule', changed: true, bytesRaw: 40960n, state: 'parsed' },
    { id: 8640n, instanceId: 410n, instance: 'apache (:8443)', node: 'lb-legacy-04', cluster: 'legacy', capturedAt: '2026-08-21T02:40:00Z', trigger: 'schedule', changed: false, bytesRaw: 71680n, state: 'stale' },
    { id: 8600n, instanceId: 120n, instance: 'nginx (:80)', node: 'edge-iad3-01', cluster: 'edge-iad3', capturedAt: '2026-08-30T08:05:00Z', trigger: 'schedule', changed: false, bytesRaw: 20480n, state: 'parsed' },
  ],
})

export const snapshotFileFixture = pb(SnapshotFileResponseSchema, {
  snapshot: { id: 8801n, capturedAt: '2026-08-30T09:35:00Z', degraded: true, degradedReason: '2 includes unreadable', fileCount: 14, bytesRaw: 184320n, parseState: 'parsed' },
  instance: { id: 900n, nodeId: 41n, clusterId: 2n, vendor: 'nginx', displayName: 'nginx (:443)', version: '1.24.0', mainConfigPath: '/etc/nginx/nginx.conf', nodeDisplayName: 'app-iad3-17', siteCount: 31, routeCount: 214, certCount: 6, lastCaptured: '2026-08-30T09:35:00Z', clusterName: 'app-iad3', parseState: 'parsed', state: 'drift' },
  file: { id: 9102n, kind: 'include', path: '/etc/nginx/conf.d/checkout.conf', bytesRaw: 2048n },
  hasAnchor: true,
  byteStart: 384,
  lineStart: 10,
  lineEnd: 13,
  body: [
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
