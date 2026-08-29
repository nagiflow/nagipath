import { EuiBadge, EuiFlexGroup, EuiFlexItem, EuiLoadingChart, EuiPageTemplate, EuiPanel, EuiSpacer, EuiText, EuiTitle } from '@elastic/eui'
import { useSearchParams } from 'react-router-dom'
import { useProbeDetail } from '../../api/queries/trace'

// Ported from internal/web/templates/probe.html against
// GET /api/ui/trace/probe/{id} (internal/api/probe.go): one stored probe's
// full detail — request, response stats, hop evidence, and matched log lines.
export function ProbeDetailPage() {
  const [params] = useSearchParams()
  const id = params.get('probe') ?? ''
  const { data, isPending, isError, error } = useProbeDetail(id)

  if (!id) {
    return <EuiPageTemplate.EmptyPrompt title={<h2>No probe selected</h2>} body={<p>Open this page with a <code>?probe=</code> id, or pick one from <a href="/trace/history">Probe history</a>.</p>} />
  }
  if (isPending) return <EuiLoadingChart size="xl" />
  if (isError) return <EuiPageTemplate.EmptyPrompt iconType="alert" color="danger" title={<h2>Could not load this probe</h2>} body={<p>{error.message}</p>} />
  if (!data?.probe) return null

  const p = data.probe
  return (
    <>
      <EuiFlexGroup alignItems="center">
        <EuiFlexItem><EuiTitle size="m"><h1>Probe {p.correlationToken.slice(0, 6)}</h1></EuiTitle></EuiFlexItem>
        <EuiFlexItem grow={false}><a href={`/trace?url=${encodeURIComponent(p.url)}`}>Open trace</a></EuiFlexItem>
      </EuiFlexGroup>
      <EuiSpacer />

      <EuiPanel>
        <EuiText size="s">
          <p>entry: {p.url}</p>
          <p>actor: {p.actorUsername || '—'} · source: {p.originHost || '—'}</p>
          <p>method: {p.method}{p.redirectCount > 0 && ` · ${p.redirectCount} redirect(s)`}</p>
          <p>status: {p.status || 'no response'}{Number(p.durationMs) > 0 && ` · ${p.durationMs} ms`}</p>
          <p>requested: {new Date(p.requestedAt).toLocaleString()}</p>
          <p>hops: {p.verifiedCount} verified, {p.stillInferred} still inferred, of {p.logCapableCount} log-capable</p>
        </EuiText>
      </EuiPanel>
      <EuiSpacer />

      <EuiPanel>
        <EuiTitle size="xs"><h2>Hop evidence</h2></EuiTitle>
        <EuiText size="xs" color="subdued">{data.stateChanges} state change(s)</EuiText>
        <EuiSpacer size="s" />
        <table style={{ width: '100%', fontSize: 12 }}>
          <thead><tr style={{ textAlign: 'left' }}><th>Node</th><th>Vendor</th><th>Evidence</th><th>Before</th><th>After</th></tr></thead>
          <tbody>
            {data.hops.map((h, i) => (
              <tr key={i}>
                <td>{h.nodeName}</td><td>{h.vendor}</td><td>{h.evidence}</td>
                <td><EuiBadge color="primary">{h.before.toUpperCase()}</EuiBadge></td>
                <td><EuiBadge color={h.after === 'verified' ? 'success' : 'warning'}>{h.after.toUpperCase()}</EuiBadge></td>
              </tr>
            ))}
          </tbody>
        </table>
      </EuiPanel>

      {data.gaps.length > 0 && (
        <>
          <EuiSpacer />
          <EuiPanel>
            <EuiTitle size="xs"><h2>Log format gaps</h2></EuiTitle>
            <EuiText size="xs" color="subdued">vendor log formats that cannot prove what this probe needed</EuiText>
            <EuiSpacer size="s" />
            {data.gaps.map((g, i) => (
              <EuiText size="s" key={i}>{g.gapVendor}: {g.gapNote} — add <code>{g.gapDirective}</code></EuiText>
            ))}
          </EuiPanel>
        </>
      )}

      {data.logLines.length > 0 && (
        <>
          <EuiSpacer />
          <EuiPanel>
            <EuiTitle size="xs"><h2>Matched log lines</h2></EuiTitle>
            <EuiSpacer size="s" />
            <div style={{ fontFamily: 'monospace', fontSize: 12 }}>
              {data.logLines.map((l, i) => (
                <div key={i} style={{ marginBottom: 6 }}>
                  <div style={{ color: '#888' }}>{l.nodeName} · {l.logPath}</div>
                  <div>{l.rawLine}</div>
                </div>
              ))}
            </div>
          </EuiPanel>
        </>
      )}
    </>
  )
}
