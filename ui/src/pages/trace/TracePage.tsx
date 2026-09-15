import { useMemo, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useSession } from '../../api/queries/session'
import { usePostTrace, useProbeRun, useStartProbe, useTrace } from '../../api/queries/trace'
import type { TraceHopPB, ProbedHopPB, TraceRecent } from '../../api/pb/nagipath/api/v1/trace_pb'
import { StateBadge } from '../../components/shared/StateBadge'
import { PageHeader } from '../../components/shared/PageHeader'
import { PanelHeader } from '../../components/shared/PanelHeader'
import {
  Badge, Button, CallOut, CodeBlock, EmptyPrompt, Field, Loading, Panel,
  PanelFooter, Select, Table, type Column,
} from '../../components/ui'

// design/'s Object column: the site or route the hop matched, formatted the way
// internal/api/trace.go's traceProv formats it for the Provenance header.
// The band's subject: the node the hop landed on, or the address it left for.
function nodeOf(h: TraceHopPB): string {
  return h.isExternal ? h.externalTarget : h.instNodeName
}

function objectOf(h: TraceHopPB): string {
  if (h.routePattern) return `${h.routeKind} ${h.routePattern}`.trim()
  if (h.siteName) return `${h.siteKind} ${h.siteName}`.trim()
  return ''
}

// design/'s "Rules fired" table is one band per hop over the rules that hop
// fired. The hop's node, vendor, object and confidence are the band; only the
// rule, its class and its evaluation number differ row to row.
type FiredRow = { key: string; ruleId: string; rule: string; cls: string }
type HopGroup = { hop: TraceHopPB; rows: FiredRow[] }

function hopGroups(hops: TraceHopPB[]): HopGroup[] {
  return hops.map((h) => ({
    hop: h,
    rows: h.firedRules.map((r, i) => ({
      key: `${h.ordinal}-${r.ruleId}-${i}`,
      ruleId: String(r.ruleId),
      rule: `${r.directive} ${r.args}`.trim(),
      cls: r.actionClass,
    })),
  }))
}

// The dot-grid canvas of design/'s screen 2a Path graph.
const graphCanvas: React.CSSProperties = {
  flex: 1, minHeight: 0, overflow: 'auto', padding: '10px 12px',
  display: 'flex', flexDirection: 'column', alignItems: 'center',
  background: 'radial-gradient(#e7ebf2 1px, transparent 1px)', backgroundSize: '14px 14px',
}

// Ported from internal/web/templates/trace.html against GET/POST
// /api/trace, POST /api/trace/probe and GET /api/trace/run/{id}
// (internal/api/{trace,probelive}.go), laid out as design/'s screen 2a: the
// 434px dot-grid path graph with its legend strip beside the flat "Rules fired"
// table and the Provenance excerpt. The graph renders design/'s literal
// .node/.edge markup rather than an EUI approximation — see theme.css.
// Omitted from that screen: the "Saved traces · 14"/"Share"/"Export" title
// actions and the "Headers · 2"/"Snapshot: current" query-bar selects (a trace
// is computed from the current snapshot against a URL — nothing is saved,
// shared, exported, header-conditioned or time-travelled), the graph's
// "Fit"/"−"/"+" zoom and "Member 1 of 4" select (the graph is a ≤40-node flex
// stack, not a canvas, and cluster members are not walked separately), the pool
// node with its member list and "Add node" button (trace.Walk emits one hop per
// instance it reaches; upstream pool membership lives on the node page's
// Upstreams tab), "Diff vs previous" and the provenance "Candidates not chosen"
// chips (the engine records the rule it chose, not the ones it rejected).
export function TracePage() {
  const [params, setParams] = useSearchParams()
  const { data: session } = useSession()
  const url = params.get('url') ?? ''
  const [urlInput, setUrlInput] = useState(url)
  const method = params.get('method') ?? 'GET'
  const runId = params.get('run') ?? ''
  const prov = params.get('prov') ?? ''
  const [cls, setCls] = useState('')
  const [hopFilter, setHopFilter] = useState('')
  const [folded, setFolded] = useState<Set<string>>(new Set())
  const toggleFold = (label: string) => setFolded((cur) => {
    const next = new Set(cur)
    if (next.has(label)) next.delete(label); else next.add(label)
    return next
  })

  const { data, isPending, isError, error } = useTrace({
    url, scheme: params.get('scheme') ?? '', hostname: params.get('hostname') ?? '',
    path: params.get('path') ?? '', port: params.get('port') ?? '', method, run: runId, prov,
  })
  const postTrace = usePostTrace()
  const startProbe = useStartProbe()
  const { data: liveRun } = useProbeRun(runId)

  const submit = (m: string) => {
    setParams((p) => { p.set('url', urlInput); p.set('method', m); p.delete('run'); p.delete('prov'); return p })
    postTrace.mutate({ url: urlInput, method: m })
  }
  const runProbe = () => {
    startProbe.mutate({ url: data?.url || urlInput, method, trace: data?.traceId ? Number(data.traceId) : undefined }, {
      onSuccess: (res) => setParams((p) => { p.set('run', String(res.runId)); return p }),
    })
  }
  const pickProv = (ruleId: string) => setParams((p) => { p.set('prov', ruleId); return p })

  const levels = useMemo(() => {
    const byLevel = new Map<number, TraceHopPB[]>()
    for (const h of data?.hops ?? []) {
      if (!byLevel.has(h.level)) byLevel.set(h.level, [])
      byLevel.get(h.level)!.push(h)
    }
    return [...byLevel.entries()].sort((a, b) => a[0] - b[0])
  }, [data?.hops])

  const groups = useMemo(() => hopGroups(data?.hops ?? []), [data?.hops])
  const allRows = groups.flatMap((g) => g.rows)
  const classes = [...new Set(allRows.map((r) => r.cls).filter(Boolean))].sort()
  // A hop left with nothing by a class filter is not an answer to that filter;
  // a hop that fired nothing at all (an external target) still is.
  const shownGroups = groups
    .filter((g) => !hopFilter || g.hop.label === hopFilter)
    .map((g) => ({ hop: g.hop, rows: cls ? g.rows.filter((r) => r.cls === cls) : g.rows }))
    .filter((g) => g.rows.length > 0 || !cls)
  const shown = shownGroups.reduce((n, g) => n + (folded.has(g.hop.label) ? 0 : g.rows.length), 0)
  const foldedCount = shownGroups.filter((g) => folded.has(g.hop.label)).length

  const recentColumns: Column<TraceRecent>[] = [
    { name: 'When', width: 170, render: (r) => <span className="m mu">{new Date(r.computedAt).toLocaleString()}</span> },
    { name: 'URL', render: (r) => <a className="m" href={`/trace?url=${r.scheme}://${r.hostname}${r.path}`}>{r.scheme}://{r.hostname}{r.path}</a> },
    { name: 'Hops', width: 60, render: (r) => <span className="m">{r.hopCount}</span> },
    { name: 'Confidence', width: 96, render: (r) => (r.confidence ? <StateBadge state={r.confidence} /> : null) },
    { name: 'Stopped', width: 200, render: (r) => <span className="m mus">{r.terminalReason}</span> },
  ]

  const probedColumns: Column<ProbedHopPB>[] = [
    { name: 'Hop', width: 130, render: (r) => <span className="m mu">{r.label}</span> },
    { name: 'Evidence', render: (r) => <span className="m">{r.evidence}</span> },
    { name: 'Before', width: 88, render: (r) => <StateBadge state={r.before} /> },
    { name: 'After', width: 96, render: (r) => <StateBadge state={r.after} /> },
  ]

  return (
    <>
      <PageHeader
        title="Trace"
        actions={data && data.asked && session?.user?.role === 'admin' && Number(data.probesCount) > 0 && (
          <Button small href={`/trace/history?url=${encodeURIComponent(data.url)}`}>
            Probe history · {data.probesCount}
          </Button>
        )}
      />

      <div className="qbar">
        <Select options={[{ value: 'GET', text: 'GET' }, { value: 'HEAD', text: 'HEAD' }]}
          value={method} onChange={(v) => setParams((p) => { p.set('method', v); return p })} />
        <Field grow placeholder="https://payments.corp.example/api/v2/charge" value={urlInput}
          onChange={setUrlInput} onEnter={() => submit(method)} />
        <Button primary loading={postTrace.isPending} onClick={() => submit(method)}>Trace</Button>
        {data?.asked && session?.user?.role === 'admin' && (
          <span title={`sends one ${method} to this URL, then reads each hop's access log`}>
            <Button loading={startProbe.isPending} onClick={runProbe}>Probe</Button>
          </span>
        )}
      </div>

      {data?.asked && (
        <div className="row" style={{ padding: '8px 16px 0', alignItems: 'center', flex: 'none' }}>
          <span className="m mu">
            {data.hops.length} hop{data.hops.length === 1 ? '' : 's'} · {data.firedCount} of {data.scopeCount} rules fired
          </span>
          <div style={{ flex: 1 }} />
          {Number(data.verifiedCount) > 0 && <Badge cls="v">{data.verifiedCount} VERIFIED</Badge>}
          {Number(data.inferredCount) > 0 && <Badge cls="i">{data.inferredCount} INFERRED</Badge>}
          {Number(data.degradedCount) > 0 && <Badge cls="d">{data.degradedCount} DEGRADED</Badge>}
          {Number(data.externalCount) > 0 && <Badge cls="e">{data.externalCount} EXTERNAL</Badge>}
        </div>
      )}

      <div className="bd">
        {isPending && <Loading label="Loading trace…" />}
        {isError && <div className="m" style={{ color: '#a1231c' }}>{error.message}</div>}

        {data?.empty && !data.asked && (
          <EmptyPrompt title="Nothing is collected yet"
            body={<>A trace is answered from collected configuration, and there is none. <a href="/nodes">Add a node</a> first.</>} />
        )}

        {data && !data.asked && !data.empty && (
          data.recent.length === 0 ? (
            <EmptyPrompt title="Nothing traced yet"
              body={<>Paste a URL above and click <strong>Trace</strong> to see its path through the fleet.</>} />
          ) : (
            <Panel z>
              <PanelHeader title="Recent traces" meta="most recent first" />
              <Table<TraceRecent> items={data.recent} columns={recentColumns} rowKey={(r) => String(r.id)} />
            </Panel>
          )
        )}

        {data?.asked && (
          <div className="row" style={{ flex: 1, minHeight: 0, alignItems: 'stretch' }}>
            <div className="col" style={{ flex: '0 0 434px', minWidth: 0 }}>
              <Panel z style={{ flex: 1, minHeight: 0 }}>
                <PanelHeader title="Path graph" meta={`${data.hops.length} hop${data.hops.length === 1 ? '' : 's'}`} />
                <div style={graphCanvas}>
                  <div className="node" style={{ width: 296 }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                      <span className="lbl">Entry</span>
                      <Badge cls="n">{data.method || 'GET'}</Badge>
                    </div>
                    {/* No DNS/VIP resolution is collected — the entry node states the
                        request as asked, which is what the walk started from. */}
                    <div className="m" style={{ marginTop: 3, fontWeight: 500 }}>
                      {data.hostname}{data.port > 0 ? `:${data.port}` : ''}{data.path}
                    </div>
                  </div>

                  {levels.map(([level, hops]) => (
                    <div key={level} style={{ width: '100%', display: 'flex', flexDirection: 'column', alignItems: 'center' }}>
                      <div className="edge">
                        <span>│</span>
                        {hops[0].inboundPath && (
                          <span>
                            {hops[0].inboundPath}
                            {hops[0].effectivePath && hops[0].effectivePath !== hops[0].inboundPath && ` → ${hops[0].effectivePath}`}
                          </span>
                        )}
                        <span>▼</span>
                      </div>
                      <div className="col" style={{ width: '100%', gap: 6, alignItems: 'center' }}>
                        {hops.map((h) => {
                          const on = !!prov && h.firedRules.some((r) => String(r.ruleId) === prov)
                          return (
                            <div key={h.ordinal}
                              className={`node${h.isExternal ? ' ext' : on ? ' on' : ''}`}
                              style={{ width: h.isExternal ? 296 : 334, cursor: h.firedRules.length > 0 ? 'pointer' : undefined }}
                              onClick={() => h.firedRules.length > 0 && pickProv(String(h.firedRules[0].ruleId))}>
                              <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                                <span className="m" style={{ fontWeight: 600 }}>{h.isExternal ? h.externalTarget : h.instNodeName}</span>
                                <StateBadge state={h.isExternal ? 'external_hop' : h.confidence} />
                                <span className="m mus" style={{ marginLeft: 'auto' }}>hop {h.label}</span>
                              </div>
                              {h.isExternal ? (
                                h.externalReason && <div className="m mus" style={{ marginTop: 3 }}>{h.externalReason}</div>
                              ) : (
                                <>
                                  <div className="m mu" style={{ marginTop: 3 }}>
                                    {h.instVendor}{h.listenerPort > 0 ? `:${h.listenerPort}` : ''}
                                    {h.siteName && ` · ${h.siteKind} ${h.siteName}`}
                                    {h.routePattern && ` · ${h.routeKind} ${h.routePattern}`}
                                  </div>
                                  <div className="m mus">
                                    {h.firedRules.length} rule{h.firedRules.length === 1 ? '' : 's'}
                                    {h.clusterName && ` · ${h.clusterName}`}
                                  </div>
                                </>
                              )}
                            </div>
                          )
                        })}
                      </div>
                    </div>
                  ))}
                </div>
                <div style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '7px 12px', borderTop: '1px solid #edf0f5' }}>
                  <span className="lbl">Legend</span>
                  <Badge cls="v">verified</Badge>
                  <Badge cls="i">inferred</Badge>
                  <Badge cls="d">degraded</Badge>
                  <Badge cls="e">external</Badge>
                </div>
              </Panel>
            </div>

            {/* The design's right column is two panels; a real trace can also carry
                entry candidates, a live probe and the last probe's evidence, so the
                column scrolls rather than squeezing the two that matter. */}
            <div className="col" style={{ flex: 1, minWidth: 0, overflow: 'auto' }}>
              <Panel z style={{ flex: '2 1 0', minHeight: 200 }}>
                <PanelHeader
                  title="Rules fired"
                  meta="evaluation order"
                  actions={
                    <>
                      <Select
                        options={[{ value: '', text: 'Class: all' }, ...classes.map((c) => ({ value: c, text: `Class: ${c}` }))]}
                        value={cls} onChange={setCls} />
                      <Select
                        options={[{ value: '', text: 'Hop: all' }, ...groups.map((g) => ({ value: g.hop.label, text: `Hop: ${g.hop.label}` }))]}
                        value={hopFilter} onChange={setHopFilter} />
                      <Button small subtle
                        onClick={() => navigator.clipboard?.writeText(shownGroups.flatMap((g) =>
                          g.rows.map((r) => `${g.hop.label}\t${nodeOf(g.hop)}\t${objectOf(g.hop)}\t${r.rule}\t${r.cls}`)).join('\n'))}>
                        Copy
                      </Button>
                    </>
                  }
                />
                <div style={{ flex: 1, minHeight: 0, overflow: 'auto' }}>
                  <table className="t">
                    <thead>
                      <tr>
                        <th style={{ width: 30 }}>#</th>
                        <th>Rule</th>
                        <th style={{ width: 70 }}>Class</th>
                        <th style={{ width: 92 }}>State</th>
                      </tr>
                    </thead>
                    {shownGroups.map((g) => {
                      const open = !folded.has(g.hop.label)
                      const state = g.hop.isExternal ? 'external_hop' : g.hop.confidence
                      return (
                        <tbody key={g.hop.label}>
                          <tr onClick={() => toggleFold(g.hop.label)} style={{ cursor: 'pointer' }}>
                            <td colSpan={4} style={{ background: '#f7f8fc', borderTop: '1px solid #d3dae6', borderBottom: '1px solid #d3dae6' }}>
                              <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                                <span className="m" style={{ fontWeight: 600 }}>
                                  {open ? '▾' : '▸'} hop {g.hop.label} · {nodeOf(g.hop)}
                                </span>
                                <span className="m mus">
                                  {[
                                    g.hop.instVendor,
                                    objectOf(g.hop),
                                    g.rows.length === 0
                                      ? 'no rule recorded'
                                      : `${g.rows.length} rule${g.rows.length === 1 ? '' : 's'}${open ? '' : ' collapsed'}`,
                                  ].filter(Boolean).join(' · ')}
                                </span>
                                <div style={{ flex: 1 }} />
                                <StateBadge state={state} />
                              </div>
                            </td>
                          </tr>
                          {open && g.rows.map((r, i) => (
                            <tr
                              key={r.key}
                              className={r.ruleId === prov ? 'hl' : i % 2 === 1 ? 'zz' : undefined}
                              onClick={() => pickProv(r.ruleId)}
                              style={{ cursor: 'pointer' }}
                            >
                              <td className="m mus">{i + 1}</td>
                              <td className="m">{r.rule}</td>
                              <td className="m mu">{r.cls}</td>
                              <td><StateBadge state={state} /></td>
                            </tr>
                          ))}
                        </tbody>
                      )
                    })}
                  </table>
                  {shownGroups.length === 0 && (
                    <div className="m mu" style={{ padding: '10px 12px' }}>No rule fired on this path.</div>
                  )}
                </div>
                <PanelFooter>
                  <span className="m mus">
                    {shown} of {allRows.length} rows
                    {foldedCount > 0 && ` · ${foldedCount} hop${foldedCount === 1 ? '' : 's'} collapsed`}
                  </span>
                  <div style={{ flex: 1 }} />
                  <span className="m mus">
                    click a row to open provenance · click a hop to fold it away
                    {data.collapsedCount > 0 && ` · ${data.collapsedCount} rules not shown: global tuning and shadowed`}
                  </span>
                </PanelFooter>
              </Panel>

              {data.prov && (() => {
                const p = data.prov
                const from = p.lines[0]?.n
                const to = p.lines[p.lines.length - 1]?.n
                return (
                  <Panel z style={{ flex: '1 1 0', minHeight: 190 }}>
                    <PanelHeader
                      title="Provenance"
                      meta={`${p.instance}${p.object ? ` · ${p.object}` : ''}`}
                      actions={Number(p.fileId) > 0 && (
                        <Button small subtle href={`/snapshots/${p.snapshotId}/file/${p.fileId}?b=${p.byteStart}`}>Open file</Button>
                      )}
                    />
                    <div style={{ padding: '8px 12px', display: 'flex', flexDirection: 'column', gap: 7, flex: 1, minHeight: 0 }}>
                      <div className="m mu" style={{ display: 'flex', alignItems: 'center', gap: 14 }}>
                        <span>{p.path || '—'}</span>
                        {p.byteStart > 0 && <span className="mus">byte {p.byteStart}</span>}
                        {from && <span className="mus">lines {from}–{to}</span>}
                        {p.digest && <span className="mus">sha {p.digest.slice(0, 4)}…{p.digest.slice(-2)}</span>}
                      </div>
                      {p.lines.length > 0
                        ? <CodeBlock lines={p.lines.map((l) => l.text)} startLine={from ?? 1} highlight={(n) => n === Number(p.line)} />
                        : <div className="m mu">{p.missing}</div>}
                    </div>
                  </Panel>
                )
              })()}

              {data.entryCandidates.length > 1 && (
                <Panel z style={{ flex: 'none' }}>
                  <PanelHeader title="Entry point candidates" meta="more than one instance answers this hostname" />
                  <div style={{ padding: '6px 12px 8px' }}>
                    {data.entryCandidates.map((c) => (
                      <div key={c.instId.toString()} className="row" style={{ alignItems: 'center', padding: '3px 0' }}>
                        <a className="m" href={`/nodes/${c.nodeId}?process=${c.instId}`} style={{ flex: 1, minWidth: 0 }}>{c.instDisplayName}</a>
                        <span className="m mus">{c.instNodeAddress}:{c.listenerPort}</span>
                        {c.selected && <Badge cls="n">selected</Badge>}
                        <span className="m mus">{c.reason}</span>
                      </div>
                    ))}
                  </div>
                </Panel>
              )}

              {data.terminalReason && data.hops.length === 0 && (
                <CallOut title="Trace ended">{data.terminalReason}</CallOut>
              )}

              {runId && liveRun && !liveRun.done && (
                <Panel z style={{ flex: 'none' }}>
                  <PanelHeader title="Live probe" actions={<Badge cls="d">RUNNING</Badge>} />
                  <div style={{ padding: '2px 12px 8px' }}>
                    {liveRun.steps.map((s, i) => <div className="m mus" key={i}>{s.text}</div>)}
                    <div className="m mus">working…</div>
                  </div>
                </Panel>
              )}
              {runId && liveRun?.done && liveRun.error && (
                <CallOut color="danger" title="Probe not sent">{liveRun.error}</CallOut>
              )}
              {(!runId || liveRun?.done) && !liveRun?.error && data.lastProbe && (
                <Panel z style={{ flex: 'none' }}>
                  <PanelHeader
                    title="Last probe"
                    meta={`${data.lastProbe.method} · ${new Date(data.lastProbe.requestedAt).toLocaleString()}`}
                    actions={
                      <>
                        <Badge cls={data.lastProbe.outcome === 'completed' ? 'v' : 'd'}>{data.lastProbe.outcome.toUpperCase()}</Badge>
                        {data.lastProbe.probeId > 0 && (
                          <Button small subtle href={`/trace/probe?probe=${data.lastProbe.probeId}`}>View probe ›</Button>
                        )}
                      </>
                    }
                  />
                  <div style={{ padding: '2px 12px 8px' }}>
                    {data.lastProbe.error && <div className="m" style={{ color: '#a1231c' }}>{data.lastProbe.error}</div>}
                    {data.lastProbe.probed.length > 0 && (
                      <Table<ProbedHopPB> items={data.lastProbe.probed} columns={probedColumns} rowKey={(r) => String(r.ordinal)} />
                    )}
                  </div>
                </Panel>
              )}
            </div>
          </div>
        )}
      </div>
    </>
  )
}
