import { DashboardResponseSchema } from '../../api/pb/nagipath/api/v1/dashboard_pb'
import { pb } from '../support/mockApi'

// Numbers and cluster names are the wireframe's own sample fleet (design/
// screen 2j), so the story reads as the same screen the design shows.
const clusters = [
  { id: 1n, name: 'legacy' },
  { id: 2n, name: 'app-iad3' },
  { id: 3n, name: 'app-sfo2' },
  { id: 4n, name: 'edge-iad3' },
  { id: 5n, name: 'edge-sfo2' },
]

// 24 hourly buckets, a couple of them unhappy — the failed/degraded colours in
// the Collection runs strip only exist if some bucket carries them.
const activity = Array.from({ length: 24 }, (_, i) => ({
  label: `${String(i).padStart(2, '0')}:00`,
  total: [46, 44, 45, 47, 43, 44, 46, 48, 51, 49, 47, 46, 45, 44, 46, 47, 49, 52, 48, 46, 45, 44, 43, 47][i],
  failed: i === 17 ? 6 : 0,
  degraded: i === 9 || i === 18 ? 4 : 0,
}))

export const dashboardFixture = pb(DashboardResponseSchema, {
  nodes: 428,
  instances: 1129,
  vendorCount: 4,
  degraded: 31,
  pendingHostKeys: 3,
  okInstances: 1092,
  unparsedInstances: 6,
  pendingInstances: 12,
  certsExpiring30d: 41,
  certBindingsExpiring: 63,
  driftedInstances: 12,
  driftedClusters: 4,
  freshInstances: 1041,
  agingInstances: 54,
  staleInstances: 26,
  totalRules: 184320,
  unreachableNodes: 2,
  unreachableSince: '2026-08-30T04:12:00Z',
  oldestSnapshot: '2026-08-21T02:40:00Z',
  oldestSnapshotNode: 'lb-legacy-04',
  avgCollectionDurationSeconds: 2.1,
  activity,
  activityMax: 52,
  clusters,
  attention: [
    {
      kind: 'collection',
      tone: 'err',
      text: 'lb-legacy-04 has failed collection 6 times',
      note: 'ssh: handshake failed — host key changed',
      link: '/nodes/104',
      cluster: 'legacy',
      since: '2026-08-30T04:12:00Z',
    },
    {
      kind: 'drift',
      tone: 'deg',
      text: 'app-iad3 has 4 nodes diverging from its golden peer',
      note: 'server block for checkout.example.com differs in 4 of 312 nodes',
      link: '/drift?cluster=2',
      cluster: 'app-iad3',
      since: '2026-08-29T18:05:00Z',
    },
    {
      kind: 'certificate',
      tone: 'deg',
      text: '41 certificates expire within 30 days',
      note: '12 of them serve a hostname with no replacement in the index',
      link: '/certificates?expires=30d',
      cluster: '',
      since: '2026-08-28T09:00:00Z',
    },
    {
      kind: 'hostkey',
      tone: 'inf',
      text: '3 host keys are waiting for a decision',
      note: 'nothing is collected from a node until its key is approved',
      link: '/settings/hostkeys',
      cluster: 'edge-sfo2',
      since: '2026-08-27T11:30:00Z',
    },
  ],
  riskClusters: [
    { id: 1n, name: 'legacy', nodes: 24, fresh: 14, freshPercent: 58, drift: 0, certsLabel: '2 exp', state: 'failing', risk: 90 },
    { id: 2n, name: 'app-iad3', nodes: 312, fresh: 300, freshPercent: 96, drift: 4, certsLabel: '41 ≤30d', state: 'drift', risk: 62 },
    { id: 3n, name: 'app-sfo2', nodes: 74, fresh: 62, freshPercent: 84, drift: 3, certsLabel: '12 ≤30d', state: 'pending', risk: 48 },
    { id: 4n, name: 'edge-iad3', nodes: 12, fresh: 12, freshPercent: 100, drift: 3, certsLabel: '4 ≤30d', state: 'drift', risk: 30 },
    { id: 5n, name: 'edge-sfo2', nodes: 6, fresh: 6, freshPercent: 100, drift: 2, certsLabel: '4 ≤30d', state: 'drift', risk: 22 },
  ],
  recentTraces: [
    { id: 9001n, hostname: 'checkout.example.com', path: '/api/v2/cart', computedAt: '2026-08-30T09:41:00Z', hopCount: 3, confidence: 'verified', terminalReason: 'upstream' },
    { id: 9000n, hostname: 'www.example.com', path: '/', computedAt: '2026-08-30T09:12:00Z', hopCount: 2, confidence: 'inferred', terminalReason: 'external_hop' },
  ],
  recentCollections: [
    { id: 5501n, nodeId: 104n, nodeName: 'lb-legacy-04', trigger: 'schedule', startedAt: '2026-08-30T09:40:00Z', status: 'failed', error: 'ssh: handshake failed' },
    { id: 5500n, nodeId: 12n, nodeName: 'edge-iad3-01', trigger: 'manual', startedAt: '2026-08-30T09:38:00Z', status: 'succeeded' },
    { id: 5499n, nodeId: 41n, nodeName: 'app-iad3-17', trigger: 'schedule', startedAt: '2026-08-30T09:35:00Z', status: 'degraded', error: '2 includes unreadable' },
  ],
  expiringCertificates: [
    { id: 701n, subjectCn: 'checkout.example.com', notAfter: '2026-09-08T00:00:00Z', bindings: 12 },
    { id: 702n, subjectCn: '*.cdn.example.net', notAfter: '2026-09-14T00:00:00Z', bindings: 31 },
    { id: 703n, subjectCn: 'legacy-admin.example.com', notAfter: '2026-09-21T00:00:00Z', bindings: 2 },
  ],
})

export const emptyDashboardFixture = pb(DashboardResponseSchema, {})
