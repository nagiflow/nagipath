import { useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useProbeHistory } from '../../api/queries/trace'
import type { ProbeRecordPB } from '../../api/pb/nagipath/api/v1/probe_pb'
import { StateBadge } from '../../components/shared/StateBadge'
import { PageHeader } from '../../components/shared/PageHeader'
import { PanelHeader } from '../../components/shared/PanelHeader'
import { Badge, Button, Disclosure, Field, Loading, Panel, Select, Table, type Column } from '../../components/ui'

// "04:33:02Z" for today, "Aug 23 21:40Z" for anything older — design/'s 2w Ran
// column, which reads as a same-shift audit trail rather than six identical
// full timestamps.
function ran(iso: string): string {
  const d = new Date(iso)
  const t = d.toISOString()
  return t.slice(0, 10) === new Date().toISOString().slice(0, 10)
    ? `${t.slice(11, 19)}Z`
    : `${d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })} ${t.slice(11, 16)}Z`
}

// Ported from internal/web/templates/probe_history.html against
// GET /api/trace/history (internal/api/probe.go), laid out as design/'s screen
// 2w: the filter strip, the probe table, and the selected probe's audit record +
// state changes expanded under its own row.
// Omitted from that screen: the numbered pager — GetProbeHistory is
// cursor-paged (next_cursor), so there is no page count to render; "Older ↓"
// is what the cursor actually supports.
export function ProbeHistoryPage() {
  const [params, setParams] = useSearchParams()
  const url = params.get('url') ?? ''
  const q = params.get('q') ?? ''
  const [qInput, setQInput] = useState(q)
  const range = params.get('range') ?? '7'
  const actor = params.get('actor') ?? ''
  const outcome = params.get('outcome') ?? ''
  const probe = params.get('probe') ?? ''
  const cursor = params.get('cursor') ?? ''

  const { data, isPending, isError, error } = useProbeHistory({ url, q, range, actor, outcome, probe, cursor })

  const exportURL = `/api/trace/history?export=csv&range=${range}${url ? `&url=${encodeURIComponent(url)}` : ''}${q ? `&q=${encodeURIComponent(q)}` : ''}${actor ? `&actor=${actor}` : ''}${outcome ? `&outcome=${outcome}` : ''}`

  const submitQuery = () => setParams((p) => { p.set('q', qInput); p.delete('cursor'); return p })

  const rowLink = (p: ProbeRecordPB) => {
    const rowParams = new URLSearchParams()
    if (url) rowParams.set('url', url)
    if (q) rowParams.set('q', q)
    rowParams.set('range', range)
    if (actor) rowParams.set('actor', actor)
    if (outcome) rowParams.set('outcome', outcome)
    rowParams.set('probe', String(p.probeId))
    return `?${rowParams.toString()}`
  }

  const columns: Column<ProbeRecordPB>[] = [
    {
      name: 'Ran', width: 130,
      render: (r) => <span className="m"><Disclosure open={probe === String(r.probeId)} />{ran(r.requestedAt)}</span>,
    },
    { name: 'Entry point', render: (r) => <a className="m" href={rowLink(r)} title={r.url}>{r.url}</a> },
    { name: 'Probe id', width: 80, render: (r) => <span className="m mu">{r.token.slice(0, 6)}</span> },
    { name: 'Status', width: 64, render: (r) => <span className="m">{r.status || '—'}</span> },
    {
      name: 'State changes', width: 150,
      render: (r) => <span className="m mu">{r.changes.length > 0 ? r.changes.join('; ') : `none${r.result !== 'completed' ? ` · ${r.result}` : ''}`}</span>,
    },
    { name: 'Actor', width: 100, render: (r) => <span className="m mu">{r.actorLabel || '—'}</span> },
  ]

  return (
    <>
      <PageHeader
        title={`Probe history${url ? ` — ${url}` : ''}`}
        actions={
          <>
            {url && <Button small href={`/trace?url=${encodeURIComponent(url)}`}>Back to trace</Button>}
            {url && <Button small href="/trace/history">All probes</Button>}
            {data && data.probes.length > 0 && <Button small href={exportURL}>Export audit</Button>}
          </>
        }
      />

      <div className="qbar">
        <Field grow placeholder="filter by entry point, actor or probe id" value={qInput} onChange={setQInput} onEnter={submitQuery} />
        <Select
          options={[{ value: '7', text: 'Range: 7 days' }, { value: '30', text: 'Range: 30 days' }, { value: '90', text: 'Range: 90 days' }, { value: 'all', text: 'Range: all' }]}
          value={range} onChange={(v) => setParams((p) => { p.set('range', v); p.delete('cursor'); return p })}
        />
        {data && data.actors.length > 0 && (
          <Select
            options={[{ value: '', text: 'Actor: any' }, ...data.actors.map((a) => ({ value: a, text: a }))]}
            value={actor} onChange={(v) => setParams((p) => { p.set('actor', v); p.delete('cursor'); return p })}
          />
        )}
        <Select
          options={[{ value: '', text: 'Outcome: any' }, { value: 'completed', text: 'Completed' }, { value: 'failed', text: 'Failed' }, { value: 'blocked', text: 'Blocked' }, { value: 'running', text: 'Running' }]}
          value={outcome} onChange={(v) => setParams((p) => { p.set('outcome', v); p.delete('cursor'); return p })}
        />
        <div style={{ flex: 1 }} />
        {data && <span className="m mu">{data.total} probe(s){data.actorCount > 0 && ` · ${data.actorCount} actor(s)`}</span>}
      </div>

      <div className="bd">
        {isPending && <Loading label="Loading probe history…" />}
        {isError && <div className="m" style={{ color: '#a1231c' }}>{error.message}</div>}

        {data && (
          <Panel z style={{ flex: 1 }}>
            <PanelHeader title="Probes" meta="one request each, operator-triggered · open a row for its audit record" />
            {data.probes.length === 0 ? (
              <div className="m mus" style={{ padding: '8px 12px' }}>
                {q || actor || outcome || range !== 'all'
                  ? <>No probe matches this filter. <a href={`/trace/history?range=all${url ? `&url=${encodeURIComponent(url)}` : ''}`}>Widen it to every probe on record</a>.</>
                  : url ? <>No probe has ever been sent at this entry point. <a href={`/trace?url=${encodeURIComponent(url)}`}>Trace it</a> and use Probe to send one.</>
                    : 'No probe has been sent yet.'}
              </div>
            ) : (
              <Table<ProbeRecordPB>
                items={data.probes}
                columns={columns}
                rowKey={(r) => String(r.probeId)}
                rowClassName={(r) => (probe === String(r.probeId) ? 'hl' : undefined)}
                onRowClick={(r) => setParams((p) => {
                  if (probe === String(r.probeId)) p.delete('probe'); else p.set('probe', String(r.probeId))
                  return p
                })}
                renderExpanded={(r) => {
                  // GetProbeHistory only hydrates the row named by ?probe=, so
                  // exactly one row can be open at a time.
                  const sel = data.selected
                  if (!sel || probe !== String(r.probeId)) return null
                  return (
                    <div className="row" style={{ alignItems: 'flex-start' }}>
                      <div className="col" style={{ flex: 1, gap: 6 }}>
                        <div className="row" style={{ alignItems: 'center' }}>
                          <span className="lbl">audit record</span>
                          <Badge cls={!sel.status ? 'n' : sel.status < 400 ? 'v' : 'r'}>{sel.status || 'no response'}</Badge>
                          <div style={{ flex: 1 }} />
                          <Button small subtle href={`/trace/probe?probe=${sel.probeId}`}>Evidence</Button>
                          <Button small href={`/trace?url=${encodeURIComponent(sel.url)}`}>Open trace</Button>
                        </div>
                        <div className="kv m" style={{ display: 'grid', gridTemplateColumns: '92px 1fr', gap: '4px 8px' }}>
                          <span className="mus">entry</span><span>{sel.url}</span>
                          <span className="mus">actor</span><span>{sel.actorLabel || '—'}</span>
                          <span className="mus">source</span><span>{sel.originHost || '—'}</span>
                          <span className="mus">method</span><span>{sel.method}{sel.redirectCount > 0 && ` · ${sel.redirectCount} redirect(s)`}</span>
                          {Number(sel.durationMs) > 0 && <><span className="mus">latency</span><span>{Number(sel.durationMs)} ms</span></>}
                          {sel.logReads && <><span className="mus">log reads</span><span>{sel.logReads}</span></>}
                          <span className="mus">requested</span><span>{new Date(sel.requestedAt).toLocaleString()}</span>
                        </div>
                        {sel.error && <div className="m" style={{ color: '#a1231c' }}>{sel.error}</div>}
                      </div>
                      <div className="col" style={{ flex: 1, gap: 5 }}>
                        <span className="lbl">state changes · {sel.changeCount}</span>
                        {sel.changes.length === 0 ? (
                          <div className="m mus">This probe produced no evidence at any hop, so it raised nothing.</div>
                        ) : sel.changes.map((c, i) => (
                          <div key={i} className="fct">
                            <span className="m" style={{ width: 150 }}>{c.host}</span>
                            <StateBadge state={c.prior} />
                            {c.to ? <><span className="m mus">→</span><StateBadge state={c.to} /></> : <span className="m mus">unchanged</span>}
                          </div>
                        ))}
                      </div>
                    </div>
                  )
                }}
                pagination={{
                  kind: 'cursor',
                  note: 'read-only record',
                  from: data.from,
                  to: data.to,
                  total: data.total,
                  hasMore: data.hasMore,
                  label: 'Older ↓',
                  onLoadMore: () => setParams((p) => { p.set('cursor', data.nextCursor); return p }),
                }}
              />
            )}
          </Panel>
        )}
      </div>
    </>
  )
}
