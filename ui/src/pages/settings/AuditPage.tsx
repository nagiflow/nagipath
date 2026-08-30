import { useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useAudit } from '../../api/queries/settings'
import type { AuditEventItem } from '../../api/pb/nagipath/api/v1/settings_pb'
import { PanelHeader } from '../../components/shared/PanelHeader'
import {
  Button, Disclosure, Loading, Panel, PanelFooter, Select, Table,
  type Column,
} from '../../components/ui'
import { SettingsLayout } from './SettingsLayout'

function when(at: string): string {
  const d = new Date(at)
  return `${d.toLocaleDateString(undefined, { month: 'short', day: '2-digit' })} ${d.toLocaleTimeString()}`
}

function target(e: AuditEventItem): string {
  if (!e.targetKind) return '—'
  return e.targetLabel ? `${e.targetKind} ${e.targetLabel}` : e.targetKind
}

// Detail is written as JSON by the callers that have something to say and as an
// empty object by the ones that do not, so "{}" and "null" are noise, not a
// change worth a column.
function change(e: AuditEventItem): string {
  const d = e.detail.trim()
  if (d && d !== '{}' && d !== 'null') return d
  return e.outcome || '—'
}

// Ported from internal/web/templates/audit.html against GET /api/settings/audit
// (internal/api/settingsservice.go), laid out as design/'s screen 8h: the filter
// bar, the paged event table and the opened event's detail expanded under its
// own row. Export
// stays a plain link (?export=csv, internal/api/settings.go's getAuditCSV) since
// it is a browser download, not a fetch.
// Omitted from that screen: the "Actor: all" select (there is no distinct-actor
// query — the filter field is the actor filter, matched with LIKE, which is what
// design/'s own `actor: s.ryan` example types), "Sort: newest" (the store returns
// newest first and offers no other order — stating it as a select would imply a
// choice), the "400 day retention" figure (the window is set on Settings ›
// Retention and can be anything, including 0 for forever), and the detail's
// "before"/"after" rows (an event records one detail string, not a value pair —
// the row shows what was written).
export function AuditPage() {
  const [params, setParams] = useSearchParams()
  const actor = params.get('actor') ?? ''
  const action = params.get('action') ?? ''
  const range = params.get('range') ?? '7d'
  const page = Number(params.get('page') ?? '1')
  const perPage = Number(params.get('per_page') ?? '50')
  const { data, isPending, isError, error } = useAudit({ actor, action, range, page, perPage })
  const [search, setSearch] = useState(actor)
  const [picked, setPicked] = useState('')
  const [copied, setCopied] = useState(false)

  const events = data?.events ?? []
  const key = (e: AuditEventItem) => `${e.at}-${e.action}-${e.actorLabel}`
  const sel = events.find((e) => key(e) === picked)

  const set = (k: string) => (v: string) => setParams((p) => {
    if (v) p.set(k, v); else p.delete(k)
    p.delete('page')
    return p
  })
  const goto = (n: number) => setParams((p) => { p.set('page', String(n)); return p })
  const from = data && data.total > 0 ? (data.page - 1) * data.perPage + 1 : 0
  const to = data ? Math.min(data.page * data.perPage, data.total) : 0

  const columns: Column<AuditEventItem>[] = [
    {
      name: 'When', width: 160,
      render: (e) => <span className="m"><Disclosure open={key(e) === picked} />{when(e.at)}</span>,
    },
    { name: 'Actor', width: 150, render: (e) => <span className="m">{e.actorLabel || 'system'}</span> },
    { name: 'Action', width: 180, render: (e) => <span className="m mu">{e.action}</span> },
    { name: 'Target', width: 220, render: (e) => <span className="m mu">{target(e)}</span> },
    { name: 'Change', render: (e) => <span className="m mu">{change(e)}</span> },
    { name: 'Source', width: 110, render: (e) => <span className={e.sourceIp ? 'm mu' : 'm mus'}>{e.sourceIp || '—'}</span> },
  ]

  const exportURL = `/api/settings/audit?export=csv${actor ? `&actor=${encodeURIComponent(actor)}` : ''}${action ? `&action=${encodeURIComponent(action)}` : ''}&range=${range}`

  return (
    <SettingsLayout
      title="Audit log"
      actions={
        <>
          <span className="m mus">append-only · no role can edit or delete an event</span>
          <Button small href={exportURL}>Export range</Button>
        </>
      }
    >
      <div className="col" style={{ flex: 1, minHeight: 0 }}>
        <div className="qbar" style={{ border: '1px solid #d3dae6', borderRadius: 6, background: '#fff', flex: 'none', padding: '7px 10px' }}>
          <span className="fld f">
            <span className="m mus">actor</span>
            <input
              value={search}
              placeholder="username, or part of one"
              onChange={(e) => setSearch(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && set('actor')(search)}
            />
            <span className="kbd">⏎</span>
          </span>
          <Select
            options={[{ value: '', text: 'Action: all' }, ...(data?.actions ?? []).map((a) => ({ value: a, text: a }))]}
            value={action}
            onChange={set('action')}
          />
          <Select
            options={[{ value: '24h', text: 'Range: 24 hours' }, { value: '7d', text: 'Range: 7 days' },
              { value: '30d', text: 'Range: 30 days' }, { value: '90d', text: 'Range: 90 days' }]}
            value={range}
            onChange={set('range')}
          />
        </div>

        {isPending && <Loading label="Loading audit log…" />}
        {isError && <div className="m" style={{ color: '#a1231c' }}>{error.message}</div>}

        {data && (
          <Panel z style={{ flex: 1, minHeight: 0 }}>
            <PanelHeader
              title="Events"
              meta={`${data.total.toLocaleString()} in filter · expand an event for its actor, target and payload`}
              actions={
                <Select
                  options={[{ value: '50', text: '50 / page' }, { value: '100', text: '100 / page' },
                    { value: '200', text: '200 / page' }]}
                  value={String(perPage)}
                  onChange={set('per_page')}
                />
              }
            />
            {/* SettingsLayout gives its children no fixed height, so the table
                caps its own scroll — otherwise a 200-row page runs past the
                bottom of the document and takes the pager with it. */}
            <div style={{ flex: 1, minHeight: 0, maxHeight: '52vh', overflow: 'auto' }}>
              <Table
                items={events}
                columns={columns}
                rowKey={key}
                rowClassName={(e) => (sel && key(e) === key(sel) ? 'hl' : '')}
                onRowClick={(e) => { setPicked(key(e) === picked ? '' : key(e)); setCopied(false) }}
                emptyMessage="No event in this range matches the filter — an absent event is itself meaningful."
                renderExpanded={(e) => key(e) === picked && (
                  <>
                    <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 7 }}>
                      <span className="lbl">{e.action}</span>
                      <span className="m mus">{when(e.at)}</span>
                      <div style={{ flex: 1 }} />
                      <Button small onClick={() => {
                        void navigator.clipboard.writeText(JSON.stringify(e, null, 2))
                        setCopied(true)
                      }}>
                        {copied ? 'Copied' : 'Copy JSON'}
                      </Button>
                    </div>
                    <div className="kv m" style={{ display: 'grid', gridTemplateColumns: '96px 1fr', gap: '4px 8px' }}>
                      <span className="mus">actor</span>
                      <span>{e.actorLabel || 'system'}{e.sourceIp && ` · from ${e.sourceIp}`}</span>
                      <span className="mus">target</span><span>{target(e)}</span>
                      <span className="mus">outcome</span><span className={e.outcome ? undefined : 'mus'}>{e.outcome || 'not recorded'}</span>
                      <span className="mus">change</span>
                      <span className={change(e) === '—' ? 'mus' : undefined}>{change(e) === '—' ? 'nothing recorded beyond the action' : change(e)}</span>
                    </div>
                  </>
                )}
              />
            </div>
            <PanelFooter>
              <span className="m mus">{events.length} of {data.total.toLocaleString()} in filter</span>
              <div style={{ flex: 1 }} />
              {data.totalPages > 1 && (
                <span className="pg">
                  {from}–{to} of {data.total.toLocaleString()}
                  <span className="pgb" onClick={() => page > 1 && goto(page - 1)}>‹</span>
                  {Array.from({ length: data.totalPages }, (_, i) => i + 1)
                    .filter((n) => n === 1 || n === data.totalPages || Math.abs(n - data.page) <= 1)
                    .map((n, i, arr) => (
                      <span key={n}>
                        {i > 0 && n - arr[i - 1] > 1 && <span className="pgb">…</span>}
                        <span className={`pgb${n === data.page ? ' on' : ''}`} onClick={() => goto(n)}>{n}</span>
                      </span>
                    ))}
                  <span className="pgb" onClick={() => page < data.totalPages && goto(page + 1)}>›</span>
                </span>
              )}
            </PanelFooter>
          </Panel>
        )}

      </div>
    </SettingsLayout>
  )
}
