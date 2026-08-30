import { useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useClusters, useSetGoldenPeer } from '../../api/queries/clusters'
import { useDrift, useRecomputeDrift, useUnignore } from '../../api/queries/drift'
import { useSession } from '../../api/queries/session'
import type { DriftClusterGroup, DriftInstanceRow, IgnoreRule } from '../../api/pb/nagipath/api/v1/drift_pb'
import { PageHeader } from '../../components/shared/PageHeader'
import { PanelHeader } from '../../components/shared/PanelHeader'
import { StatRow } from '../../components/shared/StatTile'
import {
  Badge, Button, EmptyPrompt, Loading, Modal, Panel, PanelFooter,
  QueryBar, Select, Table, type Column,
} from '../../components/ui'

const columns: Column<DriftInstanceRow>[] = [
  {
    name: 'Node',
    render: (r) => (
      <>
        <a className="m" href={`/drift/review/${r.id}`}>{r.displayName}</a>
        {r.nodeDisplayName && r.nodeDisplayName !== r.displayName && <div className="m mus">{r.nodeDisplayName}</div>}
      </>
    ),
  },
  { name: 'Divergences', width: 110, render: (r) => <span className="m">{r.divergenceCount}</span> },
  { name: 'Objects', render: (r) => <span className="m mu">{r.objectBreakdown}</span> },
  { name: 'First seen', width: 110, render: (r) => <span className="m mu">{r.firstSeen ? new Date(r.firstSeen).toLocaleDateString(undefined, { month: 'short', day: '2-digit' }) : '—'}</span> },
  { name: '', width: 96, render: (r) => <Button small subtle href={`/drift/review/${r.id}`}>Review</Button> },
]

// design/'s 6b puts an "Ignore rules · N" action in the title bar. The rules
// have been stored and returned by GET /drift all along (drift.proto's
// IgnoreRule) with nothing rendering them — this is that list. Creating a
// rule stays where it belongs: on a finding, in the review screen.
function IgnoreRulesModal({ rules, isAdmin, onClose }: { rules: IgnoreRule[]; isAdmin: boolean; onClose: () => void }) {
  const unignore = useUnignore()
  return (
    <Modal title={`Ignore rules · ${rules.length}`} onClose={onClose} footer={<Button subtle onClick={onClose}>Close</Button>}>
      <Table<IgnoreRule>
        items={rules}
        rowKey={(r) => r.id.toString()}
        columns={[
          { name: 'Object', width: 96, render: (r) => <span className="m mu">{r.objectKind}</span> },
          { name: 'Field', width: 120, render: (r) => <span className="m">{r.field || 'any'}</span> },
          { name: 'Pattern', render: (r) => <span className="m">{r.pattern}</span> },
          { name: 'Matched', width: 76, render: (r) => <span className="m">{r.matched}</span> },
          { name: 'Reason', render: (r) => <span className="m mu">{r.reason || '—'}</span> },
          ...(isAdmin ? [{
            name: '', width: 70,
            render: (r: IgnoreRule) => <Button small subtle loading={unignore.isPending} onClick={() => unignore.mutate(Number(r.id))}>Remove</Button>,
          }] : []),
        ]}
        emptyMessage="No ignore rules. Ignore a finding in a review to add one."
      />
    </Modal>
  )
}

// nagipath has no opinion which member is correct (driftservice.go's
// SetGoldenPeer doc comment) — a human names the reference. Fetches this one
// cluster's member list on open rather than joining it into GetDrift's much
// bigger response for a control most clusters never need.
function SetBaselineModal({ clusterId, clusterName, current, onClose }: {
  clusterId: number
  clusterName: string
  current: string
  onClose: () => void
}) {
  const { data } = useClusters({ q: '', drift: 'any', sort: 'name', cluster: clusterId })
  const setGolden = useSetGoldenPeer()
  const [picked, setPicked] = useState('')
  const members = data?.selected?.memberList ?? []

  return (
    <Modal
      title={`Baseline · ${clusterName}`}
      onClose={onClose}
      footer={
        <>
          <Button subtle onClick={onClose}>Cancel</Button>
          <Button
            primary
            disabled={!picked}
            loading={setGolden.isPending}
            onClick={() => setGolden.mutate({ cluster: clusterId, instance: Number(picked) }, { onSuccess: onClose })}
          >
            {current ? 'Change baseline' : 'Set baseline'}
          </Button>
        </>
      }
    >
      {!data ? (
        <div className="m mus">Loading members…</div>
      ) : members.length === 0 ? (
        <div className="m mus">No collected member of this cluster to declare as the baseline yet.</div>
      ) : (
        <div className="col" style={{ gap: 8 }}>
          <div className="m mu">
            Every other member is compared object for object against whichever one is picked here.
          </div>
          <Select
            value={picked}
            onChange={setPicked}
            options={[
              { value: '', text: 'Select a member' },
              ...members.map((m) => ({
                value: String(m.id),
                text: m.isGolden ? `${m.displayName} · current baseline` : m.displayName,
              })),
            ]}
          />
        </div>
      )}
    </Modal>
  )
}

// Ported from internal/web/templates/drift.html against GET/POST /api/drift
// (internal/api/driftservice.go), laid out as design/'s screen 6b: the filter
// strip, four stat tiles and the cluster-grouped work queue.
// Omitted from that screen: the "Unreviewed" tile, the "Reviewed" column, the
// row checkboxes and "Review selected" (nothing records per-finding review
// state — drift_findings has no reviewed column and DriftFinding carries no
// such field, so every one of those would be a made-up number), the "Export"
// action (there is no /drift?export=csv, unlike sites/certificates/rules) and
// the pager (GetDrift returns the whole queue at once).
export function DriftPage() {
  const [params, setParams] = useSearchParams()
  const { data: session } = useSession()
  const cluster = params.get('cluster') ?? ''
  const scope = params.get('scope') ?? ''
  const [filter, setFilter] = useState('')
  const [showIgnores, setShowIgnores] = useState(false)
  const [baselineFor, setBaselineFor] = useState<DriftClusterGroup>()
  const [sort, setSort] = useState('divergences')
  const { data, isPending, isError, error } = useDrift({ cluster, scope })
  const recompute = useRecomputeDrift()

  if (isPending) return <Loading label="Loading drift…" />
  if (isError) return <EmptyPrompt danger title="Could not load drift" body={error.message} />

  const isAdmin = session?.user?.role === 'admin'
  const match = (r: DriftInstanceRow) =>
    !filter || `${r.displayName} ${r.nodeDisplayName}`.toLowerCase().includes(filter.toLowerCase())
  const groups = data.clusterGroups
    .map((g) => ({
      ...g,
      instances: g.instances.filter(match).sort((a, b) => (sort === 'node'
        ? a.nodeDisplayName.localeCompare(b.nodeDisplayName)
        : b.divergenceCount - a.divergenceCount)),
    }))
    .filter((g) => g.instances.length > 0 || !filter)
  const lastRun = data.runs[0]

  return (
    <>
      <PageHeader
        title="Drift"
        meta={lastRun?.computedAt ? `last compared ${new Date(lastRun.computedAt).toLocaleString()}` : undefined}
        actions={
          <>
            <Button small subtle onClick={() => setShowIgnores(true)}>Ignore rules · {data.ignores.length}</Button>
            {isAdmin && (
              <Button primary small loading={recompute.isPending} onClick={() => recompute.mutate({ cluster: data.allScope ? 'all' : data.scope, baseline: data.baseline })}>
                Re-diff {data.allScope ? 'every process' : 'cluster'}
              </Button>
            )}
          </>
        }
      />

      <QueryBar>
        <span className="fld f">
          <span className="m mus">filter nodes</span>
          <input value={filter} onChange={(e) => setFilter(e.target.value)} />
        </span>
        <Select
          options={[
            ...data.clusters.map((c) => ({ value: c.id.toString(), text: `Cluster: ${c.name} · ${c.members}` })),
            { value: 'all', text: 'Cluster: all · vs previous snapshot' },
          ]}
          value={cluster || (data.allScope ? 'all' : data.scope)}
          onChange={(v) => setParams((p) => { p.set('cluster', v); return p })}
        />
        <Select
          options={[
            { value: '', text: 'Scope: all objects' },
            { value: 'route', text: 'Scope: routes' },
            { value: 'upstream', text: 'Scope: upstreams' },
            { value: 'site', text: 'Scope: sites' },
            { value: 'listener', text: 'Scope: listeners' },
            { value: 'rule', text: 'Scope: rules' },
          ]}
          value={scope}
          onChange={(v) => setParams((p) => { if (v) p.set('scope', v); else p.delete('scope'); return p })}
        />
        <div style={{ flex: 1 }} />
        <span className="m mu">
          {data.instancesWithDrift.length} node{data.instancesWithDrift.length === 1 ? '' : 's'} off baseline
          {data.clusters.length > 0 && ` · ${data.clusters.length} cluster${data.clusters.length === 1 ? '' : 's'}`}
        </span>
      </QueryBar>

      <div className="bd">
        {data.empty ? (
          <EmptyPrompt title="Nothing is collected yet" body={<>Drift compares parsed configuration between processes, and there is none. <a href="/nodes/import">Import inventory</a> first.</>} />
        ) : (
          <>
            <StatRow stats={[
              { label: 'Nodes off baseline', value: data.instancesWithDrift.length, sub: `of ${data.memberCount}`, tone: data.instancesWithDrift.length > 0 ? 'warning' : undefined },
              { label: 'Divergent objects', value: data.totalDivergentObjects, sub: data.scopeFilter ? `${data.scopeFilter} only` : 'every object kind' },
              { label: 'Ignore rules', value: data.ignores.length, sub: 'suppressing matches' },
              { label: 'No baseline set', value: data.clustersWithoutBaseline, sub: 'clusters excluded', tone: data.clustersWithoutBaseline > 0 ? 'warning' : undefined },
            ]} />

            <Panel z style={{ flex: 1 }}>
              <PanelHeader
                title="Nodes off baseline"
                meta={data.allScope ? 'every process, against its previous snapshot' : `cluster ${data.clusterName || data.scope}`}
                actions={
                  <>
                    <span className="m mus">Group: cluster</span>
                    <Select value={sort} onChange={setSort}
                      options={[
                        { value: 'divergences', text: 'Sort: divergences desc' },
                        { value: 'node', text: 'Sort: node' },
                      ]} />
                  </>
                }
              />

              {data.runs.length === 0 ? (
                <div style={{ padding: '10px 12px' }}>
                  <div className="ph2">Nothing compared yet</div>
                  <div className="m mus" style={{ marginTop: 6 }}>
                    Re-diff compares {data.allScope ? 'every process' : 'every member of this cluster'} against its baseline.
                  </div>
                </div>
              ) : data.instancesWithDrift.length === 0 ? (
                <div style={{ padding: '10px 12px' }}>
                  <div className="ph2">No divergences</div>
                  <div className="m mus" style={{ marginTop: 6 }}>Every member matches its baseline object for object.</div>
                </div>
              ) : (
                <div style={{ overflow: 'auto' }}>
                  {groups.map((g) => (
                    <div key={g.clusterName}>
                      {/* design/'s full-bleed group band: which cluster, which baseline, how many differ. */}
                      <div style={{ display: 'flex', alignItems: 'center', background: '#f7f8fc', borderTop: '1px solid #d3dae6', borderBottom: '1px solid #d3dae6', padding: '5px 12px' }}>
                        <span className="m" style={{ fontWeight: 600 }}>{g.clusterName}</span>{' '}
                        {g.baselineName
                          ? <span className="m mus">baseline {g.baselineName} · {g.nodesWithDrift} of {g.totalNodes} nodes differ</span>
                          : <><span className="m mus">no baseline set</span> <Badge cls="i">EXCLUDED</Badge></>}
                        <div style={{ flex: 1 }} />
                        {isAdmin && Number(g.clusterId) > 0 && (
                          <Button small subtle onClick={() => setBaselineFor(g)}>
                            {g.baselineName ? 'Change baseline' : 'Set baseline'}
                          </Button>
                        )}
                      </div>
                      <Table<DriftInstanceRow>
                        items={g.instances}
                        columns={columns}
                        rowKey={(r) => r.id.toString()}
                        emptyMessage={g.baselineName ? 'No node in this cluster matches the filter.' : 'Nothing to compare until a baseline is set.'}
                      />
                    </div>
                  ))}
                </div>
              )}

              <PanelFooter>
                <span className="m mus">
                  {groups.reduce((n, g) => n + g.instances.length, 0)} of {data.instancesWithDrift.length}
                </span>
              </PanelFooter>
            </Panel>
          </>
        )}
      </div>

      {showIgnores && <IgnoreRulesModal rules={data.ignores} isAdmin={isAdmin} onClose={() => setShowIgnores(false)} />}
      {baselineFor && (
        <SetBaselineModal
          clusterId={Number(baselineFor.clusterId)}
          clusterName={baselineFor.clusterName}
          current={baselineFor.baselineName}
          onClose={() => setBaselineFor(undefined)}
        />
      )}
    </>
  )
}
