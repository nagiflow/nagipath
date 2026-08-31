import { useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useCollections } from '../../api/queries/settings'
import type { CollectionRow } from '../../api/pb/nagipath/api/v1/settings_pb'
import { PanelHeader } from '../../components/shared/PanelHeader'
import { StateBadge } from '../../components/shared/StateBadge'
import {
  Button, Disclosure, Loading, Panel, Select, StatRow,
  Table, type Column,
} from '../../components/ui'
import { SettingsLayout } from './SettingsLayout'

function secs(ms: number): string {
  return ms >= 1000 ? `${(ms / 1000).toFixed(1)} s` : `${ms} ms`
}

function pct(n: number, total: number): string {
  return total > 0 ? `${((n / total) * 100).toFixed(1)}%` : '—'
}

// Ported from internal/web/templates/collections.html against GET /api/collections
// (internal/api/collections.go), laid out as design/'s screen 8d: the five stat
// tiles and the filtered run table, each run expanding into what it recorded.
// Omitted from that screen: the "concurrency 25" figure beside the title (there
// is no concurrency setting anywhere in the collector or its defaults),
// "Run now ▾" (collection is triggered per node —
// POST /nodes/{id}/collect — so the button belongs on a node, not here), the
// "4 schedules" figure under Runs (there is one global interval, not a schedule
// table), the numbered pager (this endpoint pages by cursor, which cannot
// address page 3 without walking to it — "Load more" is that cursor), and the
// per-step job-log transcript with its Download (the collection row stores one
// error string, not a transcript; the expanded row shows what is recorded).
export function CollectionsPage() {
  const [params, setParams] = useSearchParams()
  const node = params.get('node') ?? ''
  const status = params.get('status') ?? ''
  const trigger = params.get('trigger') ?? ''
  const range = params.get('range') ?? '24h'
  const cursor = params.get('cursor') ?? ''
  const [search, setSearch] = useState(node)
  const [picked, setPicked] = useState<bigint | null>(null)
  const { data, isPending, isError, error } = useCollections({ node, status, trigger, range, cursor })

  const rows = data?.rows ?? []
  const sel = rows.find((r) => r.id === picked)

  const columns: Column<CollectionRow>[] = [
    {
      name: 'Started', width: 116,
      render: (r) => <span className="m"><Disclosure open={r.id === picked} />{new Date(r.startedAt).toLocaleTimeString()}</span>,
    },
    {
      name: 'Target',
      width: 220,
      render: (r) => (
        <span className="m mu">
          {r.nodeName}{r.instancesSeen > 0 && ` · ${r.instancesSeen} process${r.instancesSeen === 1 ? '' : 'es'}`}
        </span>
      ),
    },
    { name: 'Trigger', width: 110, render: (r) => <span className="m mu">{r.trigger}</span> },
    { name: 'Duration', width: 70, render: (r) => <span className="m">{secs(Number(r.durationMs))}</span> },
    { name: 'Outcome', render: (r) => <span className="m mu">{r.outcome || r.error || '—'}</span> },
    { name: 'State', width: 96, render: (r) => <StateBadge state={r.status} /> },
  ]

  const exportURL = `/api/collections?export=csv&range=${range}${node ? `&node=${encodeURIComponent(node)}` : ''}${status ? `&status=${status}` : ''}${trigger ? `&trigger=${trigger}` : ''}`
  const commitNode = () => setParams((p) => { if (search) p.set('node', search); else p.delete('node'); p.delete('cursor'); return p })
  const setFilter = (key: string) => (v: string) => setParams((p) => {
    if (v) p.set(key, v); else p.delete(key)
    p.delete('cursor')
    return p
  })

  return (
    <SettingsLayout
      title="Collection jobs"
      actions={
        <>
          {data && !data.empty && <span className="m mu">queue {data.running}</span>}
          <Button small href="/settings/collection-defaults">Edit defaults</Button>
          {data && !data.empty && <Button small href={exportURL}>Export CSV</Button>}
        </>
      }
    >
      <div className="col">
        {isPending && <Loading label="Loading collection jobs…" />}
        {isError && <div className="m" style={{ color: '#a1231c' }}>{error.message}</div>}
        {data?.empty && <div className="m mus">No nodes yet — add one under Nodes first.</div>}

        {data && !data.empty && (
          <>
            <StatRow stats={[
              { label: `Runs · ${range}`, value: data.total, sub: `${data.running} in queue` },
              { label: 'Succeeded', value: data.succeeded, sub: pct(data.succeeded, data.total) },
              { label: 'Degraded', value: data.degraded, sub: 'tooling missing — files read instead', tone: data.degraded > 0 ? 'warning' : undefined },
              { label: 'Failed', value: data.failed, sub: 'nothing stored', tone: data.failed > 0 ? 'danger' : undefined },
              { label: 'Median duration', value: secs(Number(data.medianMs)), sub: `p95 ${secs(Number(data.p95Ms))}` },
            ]} />

            <Panel z style={{ flex: 1, minWidth: 0 }}>
              <PanelHeader
                title="Job runs"
                meta="expand a run to see what it recorded"
                actions={
                  <>
                    <span className="fld" style={{ flex: '0 0 150px' }}>
                      <span className="m mus">node</span>
                      <input value={search} onChange={(e) => setSearch(e.target.value)}
                        onKeyDown={(e) => e.key === 'Enter' && commitNode()} />
                    </span>
                    <Select
                      options={[{ value: '', text: 'State: any' }, { value: 'succeeded', text: 'State: succeeded' },
                        { value: 'degraded', text: 'State: degraded' }, { value: 'failed', text: 'State: failed' },
                        { value: 'running', text: 'State: running' }]}
                      value={status} onChange={setFilter('status')}
                    />
                    <Select
                      options={[{ value: '', text: 'Trigger: any' }, { value: 'scheduled', text: 'Trigger: scheduled' },
                        { value: 'manual', text: 'Trigger: manual' }, { value: 'first_contact', text: 'Trigger: first contact' }]}
                      value={trigger} onChange={setFilter('trigger')}
                    />
                    <Select
                      options={[{ value: '24h', text: 'Range: 24 hours' }, { value: '7d', text: 'Range: 7 days' },
                        { value: '30d', text: 'Range: 30 days' }, { value: '90d', text: 'Range: 90 days' }]}
                      value={range} onChange={setFilter('range')}
                    />
                  </>
                }
              />
              <Table
                items={rows}
                columns={columns}
                rowKey={(r) => r.id.toString()}
                rowClassName={(r) => (sel && r.id === sel.id ? 'hl' : '')}
                onRowClick={(r) => setPicked(r.id === picked ? null : r.id)}
                emptyMessage="No collections match these filters."
                renderExpanded={(r) => r.id === picked && (
                  <div className="row" style={{ alignItems: 'flex-start' }}>
                    <div className="col" style={{ flex: '0 0 340px', gap: 7 }}>
                      <div className="kv m" style={{ display: 'grid', gridTemplateColumns: '96px 1fr', gap: '4px 8px' }}>
                        <span className="mus">state</span><span><StateBadge state={r.status} /></span>
                        <span className="mus">started</span><span>{new Date(r.startedAt).toLocaleString()}</span>
                        <span className="mus">duration</span><span>{secs(Number(r.durationMs))}</span>
                        <span className="mus">trigger</span><span>{r.trigger}</span>
                        <span className="mus">processes</span><span>{r.instancesSeen} found</span>
                        <span className="mus">outcome</span><span>{r.outcome || '—'}</span>
                      </div>
                      <div><Button small subtle href={`/nodes/${r.nodeId}`}>Open node</Button></div>
                    </div>
                    {r.error && (
                      <div className="code" style={{ flex: 1, minWidth: 0 }}>
                        <div className="cl on" style={{ whiteSpace: 'pre-wrap' }}>
                          <span className="no">!</span>
                          <span style={{ flex: 1, minWidth: 0 }}>{r.error}</span>
                        </div>
                      </div>
                    )}
                  </div>
                )}
                pagination={{
                  kind: 'cursor',
                  from: data.from,
                  to: data.to,
                  total: data.total,
                  hasMore: data.hasMore,
                  onLoadMore: () => setParams((p) => { p.set('cursor', data.nextCursor); return p }),
                }}
              />
            </Panel>
          </>
        )}
      </div>
    </SettingsLayout>
  )
}
