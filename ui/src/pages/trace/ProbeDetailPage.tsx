import { useState, type ReactNode } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useProbeDetail } from '../../api/queries/trace'
import type { ProbeHopEvidencePB } from '../../api/pb/nagipath/api/v1/probe_pb'
import { StateBadge } from '../../components/shared/StateBadge'
import { PageHeader } from '../../components/shared/PageHeader'
import { PanelHeader } from '../../components/shared/PanelHeader'
import {
  Badge, Bar, Button, CodeBlock, EmptyPrompt, Kv, Loading, Panel,
  PanelFooter, Select, Table, type Column,
} from '../../components/ui'

const hopColumns: Column<ProbeHopEvidencePB>[] = [
  { name: 'Hop', width: 38, render: (r) => <span className="m mu">{r.hopOrdinal}</span> },
  { name: 'Node', width: 190, render: (r) => <span className="m">{r.nodeName}</span> },
  { name: 'Vendor', width: 88, render: (r) => <span className="m mu">{r.vendor}</span> },
  { name: 'Evidence', render: (r) => <span className={r.hasGap ? 'm mus' : 'm'}>{r.evidence}</span> },
  { name: 'Before', width: 88, render: (r) => <StateBadge state={r.before} /> },
  { name: 'After', width: 96, render: (r) => <StateBadge state={r.after} /> },
]

// Ported from internal/web/templates/probe.html against
// GET /api/trace/probe/{id} (internal/api/probe.go), laid out as design/'s
// screen 2h: the Request / Audit / log-gap column beside the three response
// panels, the hop evidence table and the matched log lines.
// Omitted from that screen: the "Run probe" action and the Request panel's
// editable fields, checkboxes and Redirects/Timeout selects (this route renders
// a *stored* probe — running one is Trace's Probe button, and ProbeViewPB
// records what was sent, not a form to resend it), the "Copy directive"/"Open
// file" buttons on the gap panel (the directive is selectable text and the gap
// names a vendor log format, not a file), and "Apply to trace" (the state
// changes are already written to the trace record when the probe runs — there
// is nothing left to apply).
export function ProbeDetailPage() {
  const [params] = useSearchParams()
  const id = params.get('probe') ?? ''
  const [hopFilter, setHopFilter] = useState('')
  const { data, isPending, isError, error } = useProbeDetail(id)

  // Every pre-data state still renders the title bar and body padding, or the
  // page reads as chrome that failed to load rather than a probe that is missing.
  const shell = (body: ReactNode) => (
    <>
      <PageHeader title="Probe" actions={<Button small subtle href="/trace/history">Probe history</Button>} />
      <div className="bd">{body}</div>
    </>
  )
  if (!id) {
    return shell(<EmptyPrompt title="No probe selected" body={<>Open this page with a <code>?probe=</code> id, or pick one from <a href="/trace/history">Probe history</a>.</>} />)
  }
  if (isPending) return shell(<Loading label="Loading probe…" />)
  if (isError) return shell(<EmptyPrompt danger title="Could not load this probe" body={error.message} />)
  if (!data?.probe) return null

  const p = data.probe
  const hops = data.hops.filter((h) => !hopFilter || (hopFilter === 'gap' ? h.hasGap : !h.hasGap))
  const confirmedPct = p.logCapableCount > 0 ? Math.round((p.verifiedCount / p.logCapableCount) * 100) : 0
  const ok = p.status >= 200 && p.status < 400

  return (
    <>
      <PageHeader
        title="Probe"
        badge={<Badge cls="n">manual · audited</Badge>}
        meta={p.correlationToken ? `id ${p.correlationToken.slice(0, 6)}` : undefined}
        actions={
          <>
            <Button small subtle href="/trace/history">Probe history</Button>
            <Button small href={`/trace?url=${encodeURIComponent(p.url)}`}>Back to trace</Button>
          </>
        }
      />

      <div className="bd" style={{ flexDirection: 'row' }}>
        <div className="col" style={{ flex: '0 0 380px' }}>
          <Panel style={{ padding: '11px 12px' }}>
            <div className="lbl" style={{ marginBottom: 7 }}>Request</div>
            <Kv labelWidth={82} rows={[
              ['method', p.method || 'GET'],
              ['url', p.url],
              ['redirects', `${p.redirectCount} followed · ${p.maxRedirects} max`],
              ['correlation', p.correlationToken ? `X-Nagipath-Probe: ${p.correlationToken.slice(0, 6)}` : <span className="mus">none sent</span>],
              ['log reads', `${p.logCapableCount} log-capable hop${p.logCapableCount === 1 ? '' : 's'}`],
            ]} />
          </Panel>

          <Panel style={{ padding: '11px 12px' }}>
            <div className="lbl" style={{ marginBottom: 7 }}>Audit</div>
            <Kv labelWidth={82} rows={[
              ['actor', p.actorUsername || <span className="mus">unknown</span>],
              ['source', p.originHost || <span className="mus">—</span>],
              ['probe id', p.correlationToken || String(p.probeId)],
              ['started', p.requestedAt ? new Date(p.requestedAt).toLocaleString() : <span className="mus">—</span>],
              ['retention', p.retentionDays > 0 ? `${p.retentionDays} days` : <span className="mus">kept</span>],
            ]} />
          </Panel>

          {data.gaps.length > 0 && (
            <Panel style={{ padding: '11px 12px', flex: 1, minHeight: 0 }}>
              <div className="lbl" style={{ marginBottom: 7 }}>Log format gap · {data.gaps.map((g) => g.nodeName).join(', ')}</div>
              {data.gaps.map((g, i) => (
                <div key={i} style={{ marginBottom: 8 }}>
                  <div className="m mus" style={{ marginBottom: 4 }}>{g.gapVendor}: {g.gapNote}</div>
                  <CodeBlock lines={g.gapDirective.split('\n')} />
                </div>
              ))}
            </Panel>
          )}
        </div>

        <div className="col" style={{ flex: 1, minWidth: 0 }}>
          <div className="row" style={{ flex: 'none' }}>
            <Panel style={{ flex: 1, padding: '10px 12px' }}>
              <div className="lbl">Response</div>
              <div style={{ display: 'flex', alignItems: 'baseline', gap: 8, marginTop: 3 }}>
                <span style={{ font: '700 20px Inter', color: ok ? '#00726b' : '#a1231c' }}>{p.status || '—'}</span>
                <span className="m mu">{Number(p.durationMs)} ms{p.redirectCount > 0 && ` · ${p.redirectCount} redirect${p.redirectCount === 1 ? '' : 's'}`}</span>
              </div>
              <div className="m mus" style={{ marginTop: 3 }}>
                {p.serverHeader ? `server: ${p.serverHeader}` : 'no server header'}
                {p.viaHeader && ` · via: ${p.viaHeader}`}
              </div>
            </Panel>

            <Panel style={{ flex: 1, padding: '10px 12px' }}>
              <div className="lbl">Hops confirmed</div>
              <div style={{ display: 'flex', alignItems: 'baseline', gap: 8, marginTop: 3 }}>
                <span style={{ font: '700 20px Inter' }}>{p.verifiedCount}</span>
                <span className="m mu">of {p.logCapableCount} log-capable</span>
              </div>
              <div style={{ marginTop: 6 }}><Bar pct={confirmedPct} /></div>
            </Panel>

            <Panel style={{ flex: 1, padding: '10px 12px' }}>
              <div className="lbl">State changes</div>
              <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginTop: 5 }}>
                {data.stateChanges > 0 ? (
                  <>
                    <Badge cls="i">INFERRED</Badge>
                    <span className="m mus">→</span>
                    <Badge cls="v">VERIFIED</Badge>
                    <span className="m mu">× {data.stateChanges}</span>
                  </>
                ) : <span className="m mus">none — nothing moved off its prior confidence</span>}
              </div>
              <div className="m mus" style={{ marginTop: 4 }}>{p.stillInferred} still inferred</div>
            </Panel>
          </div>

          <Panel z>
            <PanelHeader
              title="Hop evidence"
              actions={
                <Select
                  options={[
                    { value: '', text: 'Show: all hops' },
                    { value: 'proved', text: 'Show: proved by logs' },
                    { value: 'gap', text: 'Show: log format gaps' },
                  ]}
                  value={hopFilter}
                  onChange={setHopFilter}
                />
              }
            />
            <Table items={hops} columns={hopColumns} rowKey={(r) => String(r.hopOrdinal)} emptyMessage="No hop matches this filter." />
          </Panel>

          <Panel z style={{ flex: 1 }}>
            <PanelHeader title="Matched log lines" meta="read-only · from target node" />
            <div style={{ padding: '8px 12px', flex: 1, minHeight: 0 }}>
              {data.logLines.length > 0
                ? <CodeBlock lines={data.logLines.map((l) => l.rawLine)} />
                : <div className="m mus">No log line matched this probe. Every hop stayed on its prior confidence.</div>}
            </div>
            <PanelFooter>
              <span className="m mus">
                {data.logLines.length} line{data.logLines.length === 1 ? '' : 's'} from{' '}
                {new Set(data.logLines.map((l) => l.nodeName)).size} node(s)
              </span>
            </PanelFooter>
          </Panel>
        </div>
      </div>
    </>
  )
}
