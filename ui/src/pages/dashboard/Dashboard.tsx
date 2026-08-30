import { useState } from 'react'
import { useDashboard } from '../../api/queries/dashboard'
import { PageHeader } from '../../components/shared/PageHeader'
import { PanelHeader } from '../../components/shared/PanelHeader'
import { StatRow } from '../../components/shared/StatTile'
import { Badge, Button, Loading, EmptyPrompt, Panel, Select, Table } from '../../components/ui'
import { since } from '../../lib/time'

const toneClass: Record<string, string> = { ok: 'v', deg: 'd', err: 'r', inf: 'n' }

// design/'s screen 2a's "Collection coverage" strip: a stacked freshness bar
// (fresh <6h / aging 6-24h / stale >24h / never-captured) over the fleet's
// instances, not to be confused with the "Collection runs" panel below it
// (job-run history, bucketed hourly). Every field here (freshInstances/
// agingInstances/staleInstances/oldestSnapshot/avgCollectionDurationSeconds)
// is already computed in dashboardservice.go — it just wasn't rendered yet.
function CoverageBar({ data }: { data: { instances: number; freshInstances: number; agingInstances: number; staleInstances: number; oldestSnapshot: string; oldestSnapshotNode: string } }) {
  const { instances, freshInstances, agingInstances, staleInstances } = data
  const neverCaptured = Math.max(0, instances - freshInstances - agingInstances - staleInstances)
  const pct = (n: number) => (instances > 0 ? (n / instances) * 100 : 0)
  return (
    <Panel>
      <div className="row" style={{ alignItems: 'center', flex: 'none' }}>
        <div className="col" style={{ flex: '0 0 150px', gap: 3 }}>
          <span className="lbl">Collection coverage</span>
          <span className="m mu">{freshInstances} of {instances} processes fresh</span>
        </div>
        <div className="col" style={{ flex: 1, minWidth: 0, gap: 5 }}>
          <div style={{ display: 'flex', height: 9, borderRadius: 3, overflow: 'hidden', background: '#e3e8f0' }}>
            <div style={{ flex: `0 0 ${pct(freshInstances)}%`, background: '#00726b' }} />
            <div style={{ flex: `0 0 ${pct(agingInstances)}%`, background: '#f5a700' }} />
            <div style={{ flex: `0 0 ${pct(staleInstances)}%`, background: '#c8cdd8' }} />
            <div style={{ flex: `0 0 ${pct(neverCaptured)}%`, background: '#a1231c' }} />
          </div>
          <div className="row" style={{ flex: 'none' }}>
            <span className="m mu">fresh &lt; 6h <b>{freshInstances}</b></span>
            <span className="m mu">degraded <b>{agingInstances}</b></span>
            <span className="m mu">stale &gt; 24h <b>{staleInstances}</b></span>
            <span className="m mu">never captured <b>{neverCaptured}</b></span>
            <div style={{ flex: 1 }} />
            {data.oldestSnapshot && (
              <span className="m mus">oldest snapshot {new Date(data.oldestSnapshot).toLocaleDateString()} · {data.oldestSnapshotNode}</span>
            )}
          </div>
        </div>
        <Button small subtle href="/nodes">Coverage report</Button>
      </div>
    </Panel>
  )
}

// design/'s "Collection runs" strip: internal/web's 24-hour bar chart, ported
// as plain divs — the original deliberately carried no chart library
// (dashboard.go: "There is no chart library and no axis — the count is
// printed beside the shape"), and that reasoning still applies here.
function ActivityStrip({ buckets, max }: { buckets: { label: string; total: number; failed: number; degraded: number }[]; max: number }) {
  return (
    <div style={{ display: 'flex', gap: 3, alignItems: 'flex-end', flex: 1, minHeight: 0 }}>
      {buckets.map((b) => {
        const height = max > 0 ? Math.max(2, (b.total / max) * 100) : 2
        const color = b.failed > 0 ? '#a1231c' : b.degraded > 0 ? '#8a5300' : '#0077cc'
        return (
          <div key={b.label} title={`${b.label} — ${b.total}`} style={{ flex: 1, height: '100%', display: 'flex', alignItems: 'flex-end' }}>
            <div style={{ width: '100%', height: `${height}%`, background: b.total > 0 ? color : '#e3e8f0', borderRadius: '2px 2px 0 0' }} />
          </div>
        )
      })}
    </div>
  )
}

// Dashboard is the fleet-overview screen — ported from internal/web's
// dashboard.html against GET /api/dashboard (internal/api/dashboard.go),
// same filters (cluster scope, attention severity/cluster) and same fields.
// Structure matches design/'s screen 2a exactly: coverage strip, a 6-tile
// stat row, then Needs attention (1.7fr) beside Collection runs + Cluster
// risk stacked (1fr).
export function Dashboard() {
  const [cluster, setCluster] = useState(0)
  const [severity, setSeverity] = useState('all')
  const [attentionCluster, setAttentionCluster] = useState(0)
  const { data, isPending, isError, error } = useDashboard({ cluster, severity, attentionCluster })

  if (isPending) return <Loading label="Loading dashboard…" />
  if (isError) return <EmptyPrompt danger title="Could not load the dashboard" body={error.message} />

  const okRuns = data.activity.reduce((n, b) => n + (b.total - b.failed - b.degraded), 0)
  const degradedRuns = data.activity.reduce((n, b) => n + b.degraded, 0)
  const failedRuns = data.activity.reduce((n, b) => n + b.failed, 0)

  return (
    <>
      <PageHeader
        title="Dashboard"
        actions={
          <Select
            options={[{ value: '0', text: 'Scope: all clusters' }, ...data.clusters.map((c) => ({ value: c.id.toString(), text: `Scope: ${c.name}` }))]}
            value={String(cluster)}
            onChange={(v) => setCluster(Number(v))}
          />
        }
      />
      <div className="bd">
        <CoverageBar data={data} />

        <StatRow stats={[
          { label: 'Nodes', value: data.nodes, sub: `${data.vendorCount} vendor${data.vendorCount === 1 ? '' : 's'}` },
          { label: 'Processes', value: data.instances, sub: `${data.freshInstances} fresh < 6h` },
          { label: 'Indexed rules', value: data.totalRules },
          { label: 'Certs ≤ 30d', value: data.certsExpiring30d, sub: `${data.certBindingsExpiring} bindings`, tone: data.certsExpiring30d > 0 ? 'warning' : undefined },
          { label: 'Drifted nodes', value: data.driftedInstances, sub: `${data.driftedClusters} cluster${data.driftedClusters === 1 ? '' : 's'}`, tone: data.driftedInstances > 0 ? 'warning' : undefined },
          { label: 'Unreachable', value: data.unreachableNodes, sub: data.unreachableSince ? `since ${new Date(data.unreachableSince).toLocaleTimeString()}` : undefined, tone: data.unreachableNodes > 0 ? 'danger' : undefined },
        ]} />

        <div className="row" style={{ flex: 1, minHeight: 0 }}>
          <div style={{ flex: 1.7, minWidth: 0 }}>
            <Panel z style={{ height: '100%' }}>
              <PanelHeader
                title="Needs attention"
                actions={
                  <>
                    <Select
                      options={[
                        { value: 'all', text: 'Severity: all' },
                        { value: 'err', text: 'Severity: error' },
                        { value: 'deg', text: 'Severity: degraded' },
                        { value: 'inf', text: 'Severity: info' },
                      ]}
                      value={severity}
                      onChange={setSeverity}
                    />
                    <Select
                      options={[{ value: '0', text: 'Cluster: all' }, ...data.clusters.map((c) => ({ value: c.id.toString(), text: `Cluster: ${c.name}` }))]}
                      value={String(attentionCluster)}
                      onChange={(v) => setAttentionCluster(Number(v))}
                    />
                  </>
                }
              />
              <Table
                items={data.attention}
                rowKey={(a) => `${a.kind}-${a.text}-${a.since}`}
                columns={[
                  { name: 'Kind', width: 104, render: (a) => <Badge cls={toneClass[a.tone] ?? 'n'}>{a.kind}</Badge> },
                  { name: 'Subject', render: (a) => <>{a.text}{a.note && <span className="m mus"> · {a.note}</span>}</> },
                  { name: 'Cluster', width: 150, render: (a) => a.cluster || '—' },
                  { name: 'Since', width: 84, render: (a) => <span className="m mu">{since(a.since)}</span> },
                  { name: '', width: 30, render: (a) => a.link ? <a href={a.link} className="m mus">›</a> : null },
                ]}
                emptyMessage="Nothing needs attention."
              />
            </Panel>
          </div>

          <div className="col" style={{ flex: 1, minWidth: 0 }}>
            <Panel z style={{ flex: 1 }}>
              <PanelHeader title="Collection runs" meta="last 24h" />
              <div className="col" style={{ padding: '10px 12px', flex: 1, minHeight: 0 }}>
                <ActivityStrip buckets={data.activity} max={data.activityMax} />
                <div className="row" style={{ flex: 'none', alignItems: 'center' }}>
                  <Badge cls="v">{okRuns} ok</Badge>
                  <Badge cls="d">{degradedRuns} degraded</Badge>
                  <Badge cls="r">{failedRuns} failed</Badge>
                  <div style={{ flex: 1 }} />
                  {data.avgCollectionDurationSeconds > 0 && <span className="m mus">avg {data.avgCollectionDurationSeconds.toFixed(1)} s</span>}
                </div>
              </div>
            </Panel>
            {/* ponytail: hugs its rows like design/'s 2j — stretching it left a
                bordered empty box under a short list. */}
            <Panel z>
              <PanelHeader title="Clusters at risk" />
              <Table
                items={data.riskClusters}
                rowKey={(r) => r.name}
                columns={[
                  { name: 'Cluster', render: (r) => r.name },
                  { name: 'Nodes', width: 56, render: (r) => r.nodes },
                  { name: 'Fresh', width: 76, render: (r) => `${r.freshPercent}%` },
                  { name: 'Drift', width: 56, render: (r) => r.drift },
                  { name: 'Certs', width: 70, render: (r) => r.certsLabel },
                  {
                    name: 'State', width: 86,
                    render: (r) => <Badge cls={r.state === 'ok' ? 'v' : r.state === 'failing' ? 'r' : r.state === 'drift' ? 'd' : 'n'}>{r.state}</Badge>,
                  },
                ]}
                emptyMessage="No clusters yet."
              />
            </Panel>
          </div>
        </div>
      </div>
    </>
  )
}
