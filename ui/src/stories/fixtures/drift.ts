import { DriftResponseSchema, DriftReviewResponseSchema } from '../../api/pb/nagipath/api/v1/drift_pb'
import { pb } from '../support/mockApi'

const clusters = [
  { id: 1n, name: 'legacy', members: 24 },
  { id: 2n, name: 'app-iad3', members: 312 },
  { id: 3n, name: 'app-sfo2', members: 74 },
  { id: 4n, name: 'edge-iad3', members: 12 },
  { id: 5n, name: 'edge-sfo2', members: 6 },
]

const instances = [
  { id: 900n, displayName: 'nginx (:443)', nodeDisplayName: 'app-iad3-17', divergenceCount: 4, objectBreakdown: '3 routes · 1 upstream', firstSeen: '2026-08-29T18:05:00Z' },
  { id: 902n, displayName: 'nginx (:443)', nodeDisplayName: 'app-iad3-18', divergenceCount: 4, objectBreakdown: '3 routes · 1 upstream', firstSeen: '2026-08-29T18:05:00Z' },
  { id: 904n, displayName: 'nginx (:443)', nodeDisplayName: 'app-iad3-19', divergenceCount: 2, objectBreakdown: '2 routes', firstSeen: '2026-08-30T02:11:00Z' },
]

export const driftFixture = pb(DriftResponseSchema, {
  clusters,
  clusterId: 2n,
  clusterName: 'app-iad3',
  baseline: 'golden_peer',
  baselines: [
    { kind: 'golden_peer', label: 'Golden peer · app-iad3-01' },
    { kind: 'previous', label: 'Previous snapshot' },
  ],
  scope: 'cluster',
  memberCount: 312,
  totalDivergentObjects: 12,
  clustersWithoutBaseline: 1,
  instancesWithDrift: instances,
  clusterGroups: [
    { clusterId: 2n, clusterName: 'app-iad3', baselineName: 'app-iad3-01', nodesWithDrift: 3, totalNodes: 312, instances },
    { clusterId: 4n, clusterName: 'edge-iad3', baselineName: 'edge-iad3-01', nodesWithDrift: 3, totalNodes: 12, instances: [{ id: 950n, displayName: 'haproxy (:443)', nodeDisplayName: 'edge-iad3-04', divergenceCount: 1, objectBreakdown: '1 upstream', firstSeen: '2026-08-28T22:40:00Z' }] },
    // clustersWithoutBaseline: 1 above is this one — nothing to compare yet,
    // so it carries no drifted instances, just the "Set baseline" prompt.
    { clusterId: 5n, clusterName: 'edge-sfo2', baselineName: '', nodesWithDrift: 0, totalNodes: 36, instances: [] },
  ],
  runs: [
    { id: 660n, instanceId: 900n, baselineKind: 'golden_peer', baselineLabel: 'app-iad3-01', computedAt: '2026-08-30T09:36:00Z', findingCount: 4, instanceName: 'nginx (:443)', nodeName: 'app-iad3-17', vendor: 'nginx', parserVersion: 7, ignoredCount: 1 },
    { id: 659n, instanceId: 902n, baselineKind: 'golden_peer', baselineLabel: 'app-iad3-01', computedAt: '2026-08-30T09:36:00Z', findingCount: 4, instanceName: 'nginx (:443)', nodeName: 'app-iad3-18', vendor: 'nginx', parserVersion: 7 },
  ],
  ignores: [
    { id: 20n, clusterId: 2n, objectKind: 'route', field: 'target', pattern: '*/canary/*', reason: 'canary rollout, expected to differ', createdAt: '2026-07-14T12:00:00Z', matched: 6 },
  ],
})

export const emptyDriftFixture = pb(DriftResponseSchema, {
  clusters,
  clusterId: 4n,
  clusterName: 'edge-iad3',
  baseline: 'golden_peer',
  baselines: [{ kind: 'golden_peer', label: 'Golden peer · edge-iad3-01' }],
  scope: 'cluster',
  memberCount: 12,
  empty: true,
})

export const driftReviewFixture = pb(DriftReviewResponseSchema, {
  instanceId: 900n,
  instanceDisplayName: 'nginx (:443)',
  nodeDisplayName: 'app-iad3-17',
  nodeId: 41n,
  objectCount: 4,
  baselineLabel: 'Golden peer · app-iad3-01',
  subjectTime: '2026-08-30T09:35:00Z',
  baselineTime: '2026-08-30T09:38:00Z',
  nextInstanceId: 902n,
  clusterId: 2n,
  ignoredCount: 1,
  objectGroups: [
    {
      kind: 'route',
      findings: [
        { id: 7701n, objectKind: 'route', naturalKey: 'checkout.example.com /api/v2/', change: 'modified', field: 'upstream', baselineText: 'proxy_pass http://checkout_api', subjectText: 'proxy_pass http://checkout_api_canary', actionClass: 'proxy', provenance: { path: '/etc/nginx/conf.d/checkout.conf', snapshotId: 8801n, fileId: 9102n, byteStart: 384n, link: '/snapshots/8801/file/9102?b=384' } },
        { id: 7703n, objectKind: 'route', naturalKey: 'checkout.example.com /beta/', change: 'added', field: '', subjectText: 'proxy_pass http://checkout_beta', actionClass: 'proxy', provenance: { path: '/etc/nginx/conf.d/checkout.conf', snapshotId: 8801n, fileId: 9102n, byteStart: 640n, link: '/snapshots/8801/file/9102?b=640' } },
        { id: 7704n, objectKind: 'route', naturalKey: 'www.example.com /legacy/', change: 'removed', field: '', baselineText: 'return 301 https://www.example.com/', actionClass: 'redirect', provenance: { path: '/etc/nginx/conf.d/www.conf', snapshotId: 8801n, fileId: 9103n, byteStart: 128n, link: '/snapshots/8801/file/9103?b=128' } },
      ],
    },
    {
      kind: 'upstream',
      findings: [
        { id: 7702n, objectKind: 'upstream', naturalKey: 'checkout_api_canary', change: 'added', field: '', subjectText: '10.4.12.90:8080, 10.4.12.91:8080', actionClass: 'upstream', provenance: { path: '/etc/nginx/conf.d/checkout.conf', snapshotId: 8801n, fileId: 9102n, byteStart: 1024n, link: '/snapshots/8801/file/9102?b=1024' } },
      ],
    },
  ],
})
