import { ClustersResponseSchema } from '../../api/pb/nagipath/api/v1/clusters_pb'
import { pb } from '../support/mockApi'

// The same five clusters every other fixture refers to (see fixtures/nodes.ts).
export const clustersFixture = pb(ClustersResponseSchema, {
  total: 5,
  driftFilter: 'any',
  sort: 'name',
  clusters: [
    { id: 1n, name: 'legacy', members: 34, vendor: 'apache', goldenPeerName: 'lb-legacy-01', driftCount: 6, certsExpiring30d: 11, lastCollected: '2026-08-21T02:40:00Z', instancesCollected: 20 },
    { id: 2n, name: 'app-iad3', members: 214, vendor: 'nginx', goldenPeerName: 'app-iad3-01', driftCount: 4, certsExpiring30d: 18, lastCollected: '2026-08-30T09:38:00Z', instancesCollected: 206 },
    { id: 3n, name: 'app-sfo2', members: 96, vendor: 'haproxy', goldenPeerName: 'app-sfo2-01', driftCount: 1, certsExpiring30d: 7, lastCollected: '2026-08-30T09:20:00Z', instancesCollected: 94 },
    { id: 4n, name: 'edge-iad3', members: 48, vendor: 'nginx', goldenPeerName: 'edge-iad3-01', driftCount: 1, certsExpiring30d: 3, lastCollected: '2026-08-30T09:41:00Z', instancesCollected: 48 },
    { id: 5n, name: 'edge-sfo2', members: 36, vendor: 'nginx', goldenPeerName: '', driftCount: 0, certsExpiring30d: 2, lastCollected: '2026-08-30T09:41:00Z', instancesCollected: 35 },
  ],
  // The mock layer ignores query strings (Contributing.mdx), so `?cluster=`
  // always answers with this same member list regardless of which cluster
  // asked — good enough for a story to exercise the picker, not per-cluster
  // accurate. edge-sfo2 has no golden peer yet (see above), so none is golden.
  selected: {
    id: 5n,
    name: 'edge-sfo2',
    members: 36,
    vendor: 'nginx',
    goldenPeerName: '',
    memberList: [
      { id: 960n, displayName: 'edge-sfo2-02' },
      { id: 961n, displayName: 'edge-sfo2-03', divergence: 2n },
      { id: 962n, displayName: 'edge-sfo2-04', divergence: 2n },
    ],
  },
})
