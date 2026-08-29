import { useMemo, useState } from 'react'
import {
  EuiAccordion,
  EuiBadge,
  EuiButton,
  EuiCallOut,
  EuiFieldText,
  EuiFlexGroup,
  EuiFlexItem,
  EuiLoadingChart,
  EuiPageTemplate,
  EuiPanel,
  EuiSelect,
  EuiSpacer,
  EuiText,
  EuiTitle,
} from '@elastic/eui'
import { useSearchParams } from 'react-router-dom'
import { useSession } from '../../api/queries/session'
import { usePostTrace, useProbeRun, useStartProbe, useTrace } from '../../api/queries/trace'
import type { TraceHopPB } from '../../api/pb/nagipath/api/v1/trace_pb'

const confidenceColor: Record<string, 'success' | 'warning' | 'primary' | 'default'> = {
  verified: 'success', inferred: 'primary', degraded: 'warning', external_hop: 'default',
}

function ConfidenceBadge({ state }: { state: string }) {
  return <EuiBadge color={confidenceColor[state] ?? 'default'}>{state.replace('_', ' ').toUpperCase()}</EuiBadge>
}

// Ported from internal/web/templates/trace.html against GET/POST
// /api/ui/trace, POST /api/ui/trace/probe and GET /api/ui/trace/run/{id}
// (internal/api/{trace,probelive}.go). The path graph is a stacked-column
// flexbox rather than the old dotted-canvas diagram — same information
// (levels, clusters, confidence), simpler markup; ponytail per docs/adr/0017.
export function TracePage() {
  const [params, setParams] = useSearchParams()
  const { data: session } = useSession()
  const url = params.get('url') ?? ''
  const [urlInput, setUrlInput] = useState(url)
  const method = params.get('method') ?? 'GET'
  const runId = params.get('run') ?? ''
  const prov = params.get('prov') ?? ''

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

  const levels = useMemo(() => {
    const hops = data?.hops ?? []
    const byLevel = new Map<number, TraceHopPB[]>()
    for (const h of hops) {
      if (!byLevel.has(h.level)) byLevel.set(h.level, [])
      byLevel.get(h.level)!.push(h)
    }
    return [...byLevel.entries()].sort((a, b) => a[0] - b[0])
  }, [data?.hops])

  return (
    <>
      <EuiFlexGroup alignItems="center" justifyContent="spaceBetween">
        <EuiFlexItem grow={false}><EuiTitle size="m"><h1>Trace</h1></EuiTitle></EuiFlexItem>
        {data && data.asked && session?.user?.role === 'admin' && Number(data.probesCount) > 0 && (
          <EuiFlexItem grow={false}>
            <EuiButton size="s" href={`/trace/history?url=${encodeURIComponent(data.url)}`}>
              Probe history · {data.probesCount}
            </EuiButton>
          </EuiFlexItem>
        )}
      </EuiFlexGroup>
      <EuiSpacer />

      <EuiFlexGroup gutterSize="s">
        <EuiFlexItem grow={false}>
          <EuiSelect options={[{ value: 'GET', text: 'GET' }, { value: 'HEAD', text: 'HEAD' }]}
            value={method} onChange={(e) => setParams((p) => { p.set('method', e.target.value); return p })} />
        </EuiFlexItem>
        <EuiFlexItem>
          <EuiFieldText placeholder="https://payments.corp.example/api/v2/charge" value={urlInput}
            onChange={(e) => setUrlInput(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && submit(method)} />
        </EuiFlexItem>
        <EuiFlexItem grow={false}><EuiButton fill isLoading={postTrace.isPending} onClick={() => submit(method)}>Trace</EuiButton></EuiFlexItem>
        {data?.asked && session?.user?.role === 'admin' && (
          <EuiFlexItem grow={false}>
            <EuiButton isLoading={startProbe.isPending} onClick={runProbe}
              title={`sends one ${method} to this URL, then reads each hop's access log`}>
              Probe
            </EuiButton>
          </EuiFlexItem>
        )}
      </EuiFlexGroup>
      <EuiSpacer />

      {isPending && <EuiLoadingChart size="xl" />}
      {isError && <EuiText color="danger">{error.message}</EuiText>}

      {data?.empty && !data.asked && (
        <EuiPageTemplate.EmptyPrompt title={<h2>Nothing is collected yet</h2>}
          body={<p>A trace is answered from collected configuration, and there is none. <a href="/nodes">Add a node</a> first.</p>} />
      )}

      {data && !data.asked && !data.empty && (
        <>
          {data.recent.length === 0 ? (
            <EuiPageTemplate.EmptyPrompt title={<h2>Nothing traced yet</h2>}
              body={<p>Paste a URL above and click <strong>Trace</strong> to see its path through the fleet.</p>} />
          ) : (
            <EuiPanel>
              <EuiTitle size="xs"><h2>Recent traces</h2></EuiTitle>
              <EuiSpacer size="s" />
              <table style={{ width: '100%', fontSize: 13 }}>
                <thead><tr style={{ textAlign: 'left' }}><th>When</th><th>URL</th><th>Hops</th><th>Confidence</th><th>Stopped</th></tr></thead>
                <tbody>
                  {data.recent.map((r) => (
                    <tr key={r.id.toString()}>
                      <td>{new Date(r.computedAt).toLocaleString()}</td>
                      <td><a href={`/trace?url=${r.scheme}://${r.hostname}${r.path}`}>{r.scheme}://{r.hostname}{r.path}</a></td>
                      <td>{r.hopCount}</td>
                      <td>{r.confidence && <ConfidenceBadge state={r.confidence} />}</td>
                      <td>{r.terminalReason}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </EuiPanel>
          )}
        </>
      )}

      {data?.asked && (
        <>
          <EuiText size="s" color="subdued">
            {data.hops.length} hop(s) · {data.firedCount} of {data.scopeCount} rules fired
            {Number(data.verifiedCount) > 0 && <> · <EuiBadge color="success">{data.verifiedCount} VERIFIED</EuiBadge></>}
            {Number(data.inferredCount) > 0 && <> · <EuiBadge color="primary">{data.inferredCount} INFERRED</EuiBadge></>}
            {Number(data.degradedCount) > 0 && <> · <EuiBadge color="warning">{data.degradedCount} DEGRADED</EuiBadge></>}
            {Number(data.externalCount) > 0 && <> · <EuiBadge>{data.externalCount} EXTERNAL</EuiBadge></>}
          </EuiText>
          <EuiSpacer />

          <EuiFlexGroup alignItems="flexStart">
            <EuiFlexItem grow={false} style={{ width: 380 }}>
              <EuiPanel>
                <EuiTitle size="xs"><h2>Path graph</h2></EuiTitle>
                <EuiSpacer size="s" />
                {levels.map(([level, hops], i) => (
                  <div key={level}>
                    {i > 0 && <EuiText size="s" color="subdued" style={{ textAlign: 'center', padding: '4px 0' }}>│ ▼</EuiText>}
                    <EuiFlexGroup direction="column" gutterSize="xs">
                      {hops.map((h) => (
                        <EuiFlexItem key={h.ordinal}>
                          <a href={h.firedRules.length > 0 ? `/trace?${params.toString()}&prov=${h.firedRules[0].ruleId}` : undefined}
                            style={{ textDecoration: 'none', color: 'inherit' }}>
                            <EuiPanel paddingSize="s" hasBorder color={h.isExternal ? 'subdued' : String(prov) && h.firedRules.some((r) => String(r.ruleId) === prov) ? 'primary' : 'plain'}>
                              <EuiFlexGroup gutterSize="s" alignItems="center">
                                <EuiFlexItem><EuiText size="s"><strong>{h.isExternal ? h.externalTarget : h.instNodeName}</strong></EuiText></EuiFlexItem>
                                <ConfidenceBadge state={h.isExternal ? 'external_hop' : h.confidence} />
                                <EuiFlexItem grow={false}><EuiText size="xs" color="subdued">hop {h.label}</EuiText></EuiFlexItem>
                              </EuiFlexGroup>
                              {!h.isExternal && (
                                <EuiText size="xs" color="subdued">
                                  {h.instVendor}{h.listenerPort ? ` ${h.listenerPort}` : ''}
                                  {h.siteName && ` · ${h.siteKind} ${h.siteName}`}
                                  {h.routePattern && ` · ${h.routeKind} ${h.routePattern}`}
                                </EuiText>
                              )}
                              {h.firedRules.length > 0 && <EuiText size="xs" color="subdued">{h.firedRules.length} rule(s)</EuiText>}
                            </EuiPanel>
                          </a>
                        </EuiFlexItem>
                      ))}
                    </EuiFlexGroup>
                  </div>
                ))}
              </EuiPanel>
            </EuiFlexItem>

            <EuiFlexItem>
              <EuiPanel>
                <EuiTitle size="xs"><h2>Rules fired</h2></EuiTitle>
                <EuiSpacer size="s" />
                {data.hops.filter((h) => h.firedRules.length > 0).map((h) => (
                  <EuiAccordion key={h.ordinal} id={`hop-${h.ordinal}`} initialIsOpen={h.firedRules.some((r) => String(r.ruleId) === prov)}
                    buttonContent={<span>{h.label} · {h.instNodeName} · {h.inboundPath}{h.inboundPath !== h.effectivePath && ` → ${h.effectivePath}`} · {h.firedRules.length} rule(s)</span>}>
                    <table style={{ width: '100%', fontSize: 12, marginTop: 4 }}>
                      <thead><tr style={{ textAlign: 'left' }}><th>Rule</th><th>Class</th></tr></thead>
                      <tbody>
                        {h.firedRules.map((r) => (
                          <tr key={r.ruleId.toString()} style={String(r.ruleId) === prov ? { background: 'rgba(0,119,204,0.08)' } : undefined}>
                            <td><a href={`/trace?${params.toString()}&prov=${r.ruleId}`}>{r.directive} {r.args}</a></td>
                            <td>{r.actionClass}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </EuiAccordion>
                ))}
                {data.collapsedCount > 0 && (
                  <EuiText size="xs" color="subdued" style={{ marginTop: 8 }}>
                    {data.collapsedCount} rule(s) not shown · global tuning directives and shadowed rules
                  </EuiText>
                )}
              </EuiPanel>

              {data.prov && (
                <>
                  <EuiSpacer />
                  <EuiPanel id="prov">
                    <EuiFlexGroup alignItems="center">
                      <EuiFlexItem><EuiTitle size="xs"><h2>Provenance</h2></EuiTitle><EuiText size="xs" color="subdued">{data.prov.instance}{data.prov.object && ` · ${data.prov.object}`}</EuiText></EuiFlexItem>
                      {data.prov.fileId ? <EuiFlexItem grow={false}><EuiButton size="s" href={`/snapshots/${data.prov.snapshotId}/file/${data.prov.fileId}?b=${data.prov.byteStart}`}>Open file</EuiButton></EuiFlexItem> : null}
                    </EuiFlexGroup>
                    <EuiSpacer size="s" />
                    {data.prov.lines.length > 0 ? (
                      <>
                        <EuiText size="xs" color="subdued">{data.prov.path} · byte {data.prov.byteStart} · line {data.prov.line}</EuiText>
                        <div style={{ fontFamily: 'monospace', fontSize: 12, marginTop: 6 }}>
                          {data.prov.lines.map((l) => (
                            <div key={l.n} style={{ background: l.on ? 'rgba(255,221,0,0.15)' : undefined, display: 'flex', gap: 8 }}>
                              <span style={{ color: '#888', width: 32, textAlign: 'right' }}>{l.n}</span>
                              <span>{l.text}</span>
                            </div>
                          ))}
                        </div>
                      </>
                    ) : (
                      <EuiText size="s" color="subdued">{data.prov.missing}</EuiText>
                    )}
                  </EuiPanel>
                </>
              )}

              {data.entryCandidates.length > 1 && (
                <>
                  <EuiSpacer />
                  <EuiPanel>
                    <EuiText size="xs"><strong>Entry point candidates</strong></EuiText>
                    <EuiText size="xs" color="subdued">More than one instance answers this hostname.</EuiText>
                    <EuiSpacer size="xs" />
                    {data.entryCandidates.map((c) => (
                      <EuiFlexGroup key={c.instId.toString()} gutterSize="s" alignItems="center">
                        <EuiFlexItem><a href={`/instances/${c.instId}`}>{c.instDisplayName}</a></EuiFlexItem>
                        <EuiFlexItem grow={false}><EuiText size="xs" color="subdued">{c.instNodeAddress}:{c.listenerPort}</EuiText></EuiFlexItem>
                        {c.selected && <EuiFlexItem grow={false}><EuiBadge>selected</EuiBadge></EuiFlexItem>}
                        <EuiFlexItem grow={false}><EuiText size="xs" color="subdued">{c.reason}</EuiText></EuiFlexItem>
                      </EuiFlexGroup>
                    ))}
                  </EuiPanel>
                </>
              )}

              {data.terminalReason && data.hops.length === 0 && (
                <>
                  <EuiSpacer />
                  <EuiCallOut title="Trace ended">{data.terminalReason}</EuiCallOut>
                </>
              )}

              {runId && liveRun && !liveRun.done && (
                <>
                  <EuiSpacer />
                  <EuiPanel>
                    <EuiFlexGroup alignItems="center"><EuiFlexItem><EuiTitle size="xs"><h2>Live probe</h2></EuiTitle></EuiFlexItem><EuiBadge color="warning">RUNNING</EuiBadge></EuiFlexGroup>
                    <EuiSpacer size="s" />
                    {liveRun.steps.map((s, i) => <EuiText size="s" color="subdued" key={i}>{s.text}</EuiText>)}
                    <EuiText size="s" color="subdued">working…</EuiText>
                  </EuiPanel>
                </>
              )}
              {runId && liveRun?.done && liveRun.error && (
                <>
                  <EuiSpacer />
                  <EuiCallOut color="danger" title="Probe not sent">{liveRun.error}</EuiCallOut>
                </>
              )}
              {(!runId || liveRun?.done) && !liveRun?.error && data.lastProbe && (
                <>
                  <EuiSpacer />
                  <EuiPanel>
                    <EuiFlexGroup alignItems="center">
                      <EuiFlexItem><EuiTitle size="xs"><h2>Last probe</h2></EuiTitle></EuiFlexItem>
                      <EuiBadge color={data.lastProbe.outcome === 'completed' ? 'success' : 'warning'}>{data.lastProbe.outcome.toUpperCase()}</EuiBadge>
                      <EuiFlexItem grow={false}><EuiText size="xs" color="subdued">{data.lastProbe.method} · {new Date(data.lastProbe.requestedAt).toLocaleString()}</EuiText></EuiFlexItem>
                    </EuiFlexGroup>
                    {data.lastProbe.error && <EuiText size="s" color="danger">{data.lastProbe.error}</EuiText>}
                    {data.lastProbe.probed.length > 0 && (
                      <table style={{ width: '100%', fontSize: 12, marginTop: 8 }}>
                        <thead><tr style={{ textAlign: 'left' }}><th>Hop</th><th>Instance</th><th>Evidence</th><th>Before</th><th>After</th></tr></thead>
                        <tbody>
                          {data.lastProbe.probed.map((p) => (
                            <tr key={p.ordinal}>
                              <td>{p.label}</td><td>{p.label}</td><td>{p.evidence}</td>
                              <td><ConfidenceBadge state={p.before} /></td><td><ConfidenceBadge state={p.after} /></td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    )}
                  </EuiPanel>
                </>
              )}
            </EuiFlexItem>
          </EuiFlexGroup>
        </>
      )}
    </>
  )
}
