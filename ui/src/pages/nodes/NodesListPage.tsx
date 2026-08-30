import { useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useQueryClient } from '@tanstack/react-query'
import { api } from '../../api/client'
import { useClusters, useRenameCluster, useSetGoldenPeer } from '../../api/queries/clusters'
import { useNode, useNodes } from '../../api/queries/nodes'
import { useSession } from '../../api/queries/session'
import type { ClusterListItem } from '../../api/pb/nagipath/api/v1/clusters_pb'
import type { NodeListRow } from '../../api/pb/nagipath/api/v1/nodes_pb'
import { StateBadge } from '../../components/shared/StateBadge'
import { PageHeader } from '../../components/shared/PageHeader'
import { PanelHeader } from '../../components/shared/PanelHeader'
import { StatRow } from '../../components/shared/StatTile'
import {
  Badge, Button, Checkbox, Disclosure, EmptyPrompt, Field, Loading, Panel,
  PanelFooter, QueryBar, Select,
} from '../../components/ui'

// "04:12Z · 21m" — design/'s screen 2c prints the collection time and how
// long ago it was in the same cell, which is the question the column is
// actually asked ("is this fresh?").
function collectedAt(iso: string): string {
  if (!iso) return 'never'
  const then = new Date(iso)
  const mins = Math.max(0, Math.round((Date.now() - then.getTime()) / 60000))
  const ago = mins < 60 ? `${mins}m` : mins < 1440 ? `${Math.round(mins / 60)}h` : `${Math.round(mins / 1440)}d`
  return `${then.toISOString().slice(11, 16)}Z · ${ago}`
}

// store.NodesAggregated writes 'mixed' when a node's processes sit in different
// clusters and leaves it empty when none do; design/ groups both under one band.
const NO_CLUSTER = '— no cluster'
function bandOf(r: NodeListRow): string {
  return !r.cluster || r.cluster === 'mixed' ? NO_CLUSTER : r.cluster
}

// Ported from internal/web/templates/nodes.html and clusters.html against
// GET/POST /api/nodes and /api/clusters, laid out as design/'s screen 2c: one
// tree, three levels — cluster band → node row → process row. Clusters are
// reconciled from collected configuration (ReconcileClusters), never created,
// so the band is a grouping of what was found and the whole of cluster
// administration lives on it: rename, clear baseline, open the drift report.
// ListNodes takes only `q` and returns every node at once, so the
// Vendor/Cluster/Collected facets and the Sort select filter the rows already
// in hand; Drift goes to /api/clusters, which applies it server-side.
// Omitted: the "Views:" saved-view menu and the "Columns · 8" picker (no
// endpoint behind either), and the per-instance divergence counts the old
// Clusters page listed (they need ?cluster= hydration for one cluster at a
// time — the drift report is the place that comparison belongs).
export function NodesListPage() {
  const [params, setParams] = useSearchParams()
  const q = params.get('q') ?? ''
  const [search, setSearch] = useState(q)
  const { data, isPending, isError, error } = useNodes(q)
  const { data: session } = useSession()
  const [vendor, setVendor] = useState('')
  const [cluster, setCluster] = useState('')
  const [collected, setCollected] = useState('')
  const [drift, setDrift] = useState('any')
  const [sort, setSort] = useState('collected')
  const { data: clusterData } = useClusters({ q: '', drift, sort: 'name' })
  const [folded, setFolded] = useState<Set<string>>(new Set())
  const [open, setOpen] = useState(0n)
  const [picked, setPicked] = useState<bigint[]>([])
  const queryClient = useQueryClient()
  const [collecting, setCollecting] = useState(false)

  if (isPending) return <Loading label="Loading nodes…" />
  if (isError) return <EmptyPrompt danger title="Could not load nodes" body={error.message} />

  const isAdmin = session?.user?.role === 'admin'
  const processes = data.nodes.reduce((n, r) => n + r.processCount, 0)
  const vendors = new Set(data.nodes.map((r) => r.vendor).filter(Boolean)).size
  const unclustered = data.nodes.filter((r) => bandOf(r) === NO_CLUSTER).length
  const degraded = data.nodes.filter((r) => r.consecutiveFailures > 0 && !quarantined(r, data.threshold)).length
  const captured6h = data.nodes.filter((r) => r.lastCaptured && Date.now() - new Date(r.lastCaptured).getTime() < 6 * 3600e3).length

  const vendorNames = [...new Set(data.nodes.map((r) => r.vendor).filter(Boolean))].sort()
  const clusterNames = [...new Set(data.nodes.map((r) => r.cluster).filter(Boolean))].sort()
  const age = (r: NodeListRow) => (r.lastCollection ? Date.now() - new Date(r.lastCollection).getTime() : Infinity)
  const rows = data.nodes
    .filter((r) => !vendor || r.vendor === vendor)
    .filter((r) => !cluster || r.cluster === cluster)
    .filter((r) => {
      if (!collected) return true
      if (collected === 'never') return !r.lastCollection
      if (collected === '6h') return age(r) < 6 * 3600e3
      if (collected === '24h') return age(r) < 24 * 3600e3
      return age(r) >= 24 * 3600e3
    })
    .sort((a, b) => (sort === 'name' ? a.displayName.localeCompare(b.displayName) : age(b) - age(a)))

  // The drift filter is the cluster endpoint's, so it can only narrow which
  // bands exist — a node whose cluster fell out of that answer falls with it.
  const clusters = clusterData?.clusters ?? []
  const byName = new Map(clusters.map((c) => [c.name, c]))
  const bands = new Map<string, NodeListRow[]>()
  for (const r of rows) {
    const name = bandOf(r)
    if (drift !== 'any' && name !== NO_CLUSTER && !byName.has(name)) continue
    if (drift !== 'any' && name === NO_CLUSTER) continue
    if (!bands.has(name)) bands.set(name, [])
    bands.get(name)!.push(r)
  }
  const bandNames = [...bands.keys()].sort((a, b) =>
    a === NO_CLUSTER ? 1 : b === NO_CLUSTER ? -1 : a.localeCompare(b))
  const shownNodes = [...bands.values()].reduce((n, g) => n + g.length, 0)
  const shownClusters = bandNames.filter((n) => n !== NO_CLUSTER).length

  const toggleBand = (name: string) => setFolded((cur) => {
    const next = new Set(cur)
    if (next.has(name)) next.delete(name); else next.add(name)
    return next
  })

  const bulkCollect = async () => {
    setCollecting(true)
    // ponytail: N posts, one per node — /collect is per-node and a real bulk
    // endpoint only pays off past the point a browser tab is the client.
    try {
      await Promise.all(picked.map((id) => api.postAction(`/nodes/${id}/collect`)))
    } finally {
      setCollecting(false)
      setPicked([])
      queryClient.invalidateQueries({ queryKey: ['nodes'] })
    }
  }

  const commit = () => setParams((p) => { if (search) p.set('q', search); else p.delete('q'); return p })

  return (
    <>
      <PageHeader
        title="Nodes"
        actions={isAdmin && <Button primary small href="/nodes/import">Import inventory</Button>}
      />

      <QueryBar>
        <span className="fld f">
          <span className="m mus">filter</span>
          <input value={search} onChange={(e) => setSearch(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && commit()} placeholder="name, address, vendor or cluster" />
        </span>
        <Button subtle onClick={commit}>Filter</Button>
        <Select value={vendor} onChange={setVendor}
          options={[{ value: '', text: 'Vendor: all' }, ...vendorNames.map((v) => ({ value: v, text: `Vendor: ${v}` }))]} />
        <Select value={cluster} onChange={setCluster}
          options={[{ value: '', text: 'Cluster: all' }, ...clusterNames.map((c) => ({ value: c, text: `Cluster: ${c}` }))]} />
        <Select value={collected} onChange={setCollected}
          options={[
            { value: '', text: 'Collected: any' },
            { value: '6h', text: 'Collected: < 6h' },
            { value: '24h', text: 'Collected: < 24h' },
            { value: 'stale', text: 'Collected: > 24h' },
            { value: 'never', text: 'Collected: never' },
          ]} />
        <Select value={drift} onChange={setDrift}
          options={[
            { value: 'any', text: 'Drift: any' },
            { value: 'drifted', text: 'Drift: drifted only' },
            { value: 'clean', text: 'Drift: clean' },
          ]} />
      </QueryBar>

      <div className="bd">
        <StatRow stats={[
          { label: 'Clusters', value: clusters.length, sub: 'reconciled from configuration' },
          { label: 'Nodes', value: data.total, sub: `${vendors} vendor${vendors === 1 ? '' : 's'} · ${unclustered} unclustered` },
          { label: 'Processes', value: processes, sub: `${captured6h} collected in last 6h` },
          {
            label: 'Degraded', value: degraded,
            sub: data.quarantined > 0 ? `collection failing · ${data.quarantined} quarantined` : 'collection failing',
            tone: data.quarantined > 0 ? 'danger' : degraded > 0 ? 'warning' : undefined,
          },
          { label: 'Pending first collection', value: data.neverCollected, sub: data.pending > 0 ? `${data.pending} host key(s) not approved` : undefined },
        ]} />

        {data.pending > 0 && (
          <div className="m">
            <a href="/settings/hostkeys">{data.pending} host key(s) waiting for approval →</a>
          </div>
        )}

        <Panel z style={{ flex: 1 }}>
          <PanelHeader
            title={`${shownClusters} cluster${shownClusters === 1 ? '' : 's'} · ${shownNodes} node${shownNodes === 1 ? '' : 's'}`}
            meta="expand a cluster to see its nodes, a node to see its processes"
            actions={
              <>
                <Select value={sort} onChange={setSort}
                  options={[
                    { value: 'collected', text: 'Sort: last collected' },
                    { value: 'name', text: 'Sort: name' },
                  ]} />
                {isAdmin && (
                  <Button small subtle disabled={picked.length === 0} loading={collecting} onClick={bulkCollect}>
                    Collect selected{picked.length > 0 ? ` · ${picked.length}` : ''}
                  </Button>
                )}
              </>
            }
          />
          <div style={{ flex: 1, minHeight: 0, overflow: 'auto' }}>
            <table className="t">
              <thead>
                <tr>
                  <th style={{ width: 30 }}></th>
                  <th style={{ width: 224 }}>Cluster · node · process</th>
                  <th style={{ width: 110 }}>Vendor</th>
                  <th style={{ width: 66 }}>Proc.</th>
                  <th style={{ width: 100 }}>Version</th>
                  <th style={{ width: 130 }}>Last collected</th>
                  <th>Listeners</th>
                  <th style={{ width: 100 }}>State</th>
                </tr>
              </thead>
              {bandNames.map((name) => {
                const bandNodes = bands.get(name)!
                const openBand = !folded.has(name)
                return (
                  <tbody key={name}>
                    <ClusterBand
                      name={name}
                      cluster={byName.get(name)}
                      nodes={bandNodes}
                      open={openBand}
                      isAdmin={isAdmin}
                      onToggle={() => toggleBand(name)}
                    />
                    {openBand && bandNodes.map((r) => (
                      <NodeRows
                        key={r.id.toString()}
                        row={r}
                        threshold={data.threshold}
                        open={open === r.id}
                        onToggle={() => setOpen(open === r.id ? 0n : r.id)}
                        picked={picked.includes(r.id)}
                        onPick={() => setPicked((p) => p.includes(r.id) ? p.filter((x) => x !== r.id) : [...p, r.id])}
                      />
                    ))}
                  </tbody>
                )
              })}
            </table>
            {bandNames.length === 0 && (
              <div className="m mu" style={{ padding: '10px 12px' }}>
                {q || vendor || cluster || collected || drift !== 'any' ? 'No nodes match this filter.' : 'No nodes added yet.'}
              </div>
            )}
          </div>
          <PanelFooter>
            <span className="m mus">{shownNodes} of {data.total} nodes · {processes} processes</span>
            <div style={{ flex: 1 }} />
            <span className="m mus">click a cluster to fold it away</span>
          </PanelFooter>
        </Panel>
      </div>
    </>
  )
}

// The cluster band: what was reconciled, plus the three things anyone can do to
// a cluster. Rename and Clear baseline are POSTs on the cluster itself, so they
// sit here rather than on any node under it.
function ClusterBand({ name, cluster, nodes, open, isAdmin, onToggle }: {
  name: string
  cluster?: ClusterListItem
  nodes: NodeListRow[]
  open: boolean
  isAdmin: boolean
  onToggle: () => void
}) {
  const rename = useRenameCluster()
  const golden = useSetGoldenPeer()
  const [renaming, setRenaming] = useState('')
  const procs = nodes.reduce((n, r) => n + r.processCount, 0)
  const stop = (e: React.MouseEvent) => e.stopPropagation()

  const meta = cluster
    ? [
      cluster.vendor || 'mixed vendors',
      `${nodes.length} node${nodes.length === 1 ? '' : 's'}`,
      `${procs} process${procs === 1 ? '' : 'es'}`,
      cluster.goldenPeerName ? `baseline ${cluster.goldenPeerName}` : 'no baseline',
      cluster.lastCollected ? collectedAt(cluster.lastCollected) : 'never collected',
      cluster.certsExpiring30d > 0 ? `${cluster.certsExpiring30d} cert(s) ≤30d` : '',
      open ? '' : 'collapsed',
    ].filter(Boolean).join(' · ')
    : `${nodes.length} node${nodes.length === 1 ? '' : 's'} · ${procs} process${procs === 1 ? '' : 'es'} · a process joins a cluster once a second one holds byte-identical configuration`

  return (
    <tr onClick={onToggle} style={{ cursor: 'pointer' }}>
      <td colSpan={8} style={{ background: '#f7f8fc', borderTop: '1px solid #d3dae6', borderBottom: '1px solid #d3dae6' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <span className="m" style={{ fontWeight: 600 }}>{open ? '▾' : '▸'} {name}</span>
          <span className="m mus">{meta}</span>
          <div style={{ flex: 1 }} />
          {cluster && (cluster.goldenPeerName
            ? <Badge cls={cluster.driftCount > 0 ? 'd' : 'v'}>{cluster.driftCount > 0 ? `DRIFT ${cluster.driftCount}` : 'CLEAN'}</Badge>
            : <Badge cls="n">NO BASELINE</Badge>)}
          {cluster && (
            // The band row itself folds the cluster, so its actions swallow the
            // click that would otherwise fold it.
            <span style={{ display: 'flex', alignItems: 'center', gap: 6 }} onClick={stop}>
              {isAdmin && (renaming ? (
                <>
                  <Field value={renaming} onChange={setRenaming} />
                  <Button small subtle loading={rename.isPending}
                    onClick={() => rename.mutate({ cluster: Number(cluster.id), name: renaming }, { onSuccess: () => setRenaming('') })}>
                    Save
                  </Button>
                  <Button small subtle onClick={() => setRenaming('')}>Cancel</Button>
                </>
              ) : (
                <Button small subtle onClick={() => setRenaming(cluster.name)}>Rename</Button>
              ))}
              {isAdmin && cluster.goldenPeerName && (
                <Button small subtle loading={golden.isPending} onClick={() => golden.mutate({ cluster: Number(cluster.id) })}>
                  Clear baseline
                </Button>
              )}
              <Button small subtle href={`/drift?cluster=${cluster.id}`}>Open drift report</Button>
            </span>
          )}
        </div>
      </td>
    </tr>
  )
}

function NodeRows({ row, threshold, open, onToggle, picked, onPick }: {
  row: NodeListRow
  threshold: number
  open: boolean
  onToggle: () => void
  picked: boolean
  onPick: () => void
}) {
  return (
    <>
      <tr className={picked ? 'hl' : undefined}>
        <td><Checkbox checked={picked} onChange={onPick} /></td>
        <td style={{ paddingLeft: 14 }}>
          <span className="m" style={{ fontWeight: 500 }}>
            <span onClick={onToggle} style={{ cursor: 'pointer' }}><Disclosure open={open} /></span>
            <a href={`/nodes/${row.id}`}>{row.displayName}</a>
          </span>
        </td>
        <td className="m mu">{row.vendor || '—'}</td>
        <td className="m">{row.processCount}</td>
        <td className="m mu">{row.version || '—'}</td>
        <td className={row.lastCollection ? 'm mu' : 'm mus'}>{collectedAt(row.lastCollection)}</td>
        <td className={row.listeners ? 'm mu' : 'm mus'}>{row.listeners || '—'}</td>
        <td>
          {!row.lastCollection ? <Badge cls="i">PENDING</Badge>
            : row.consecutiveFailures === 0 ? <StateBadge state="ok" />
            : <StateBadge state={quarantined(row, threshold) ? 'quarantined' : 'degraded'} title={row.lastStatus} />}
        </td>
      </tr>
      {open && <NodeProcessRows id={row.id} />}
    </>
  )
}

// design/'s 2c expands a node into its processes in place. ListNodes carries
// no per-process rows, so the open row asks GET /api/nodes/{id} for them —
// the same payload the node page uses, fetched only while a row is open.
function NodeProcessRows({ id }: { id: bigint }) {
  const { data, isPending } = useNode(Number(id), { tab: 'sites' })
  if (isPending) return <tr><td colSpan={8} className="m mus" style={{ paddingLeft: 32 }}>Loading processes…</td></tr>
  const instances = data?.instances ?? []
  if (instances.length === 0) return <tr><td colSpan={8} className="m mus" style={{ paddingLeft: 32 }}>No processes captured yet.</td></tr>
  return (
    <>
      {instances.map((p) => (
        <tr key={p.id.toString()}>
          <td></td>
          <td style={{ paddingLeft: 32 }}>
            <a className="m" href={`/nodes/${id}?process=${p.id}`}>↳ {p.displayName}</a>
          </td>
          <td className="m mu">{p.vendor || '—'}</td>
          <td></td>
          <td className="m mu">{p.version || '—'}</td>
          <td className={p.lastCaptured ? 'm mu' : 'm mus'}>{p.lastCaptured ? `${new Date(p.lastCaptured).toISOString().slice(11, 16)}Z` : 'never'}</td>
          <td className="m mu">{p.siteCount} site{p.siteCount === 1 ? '' : 's'} · {p.routeCount} route{p.routeCount === 1 ? '' : 's'}</td>
          <td><StateBadge state={p.degraded ? 'degraded' : 'ok'} /></td>
        </tr>
      ))}
    </>
  )
}

// store.Quarantined's rule, the one place the frontend has to mirror it:
// ListNodes sends the raw failure count plus the threshold rather than a
// precomputed state, so the badge has to apply it here.
function quarantined(r: NodeListRow, threshold: number): boolean {
  return r.consecutiveFailures >= (threshold || 10)
}
