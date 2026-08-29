import { EuiBadge, EuiButton, EuiFieldSearch, EuiFlexGroup, EuiFlexItem, EuiLoadingChart, EuiPanel, EuiSelect, EuiSpacer, EuiText, EuiTitle } from '@elastic/eui'
import { useSearchParams } from 'react-router-dom'
import { useProbeHistory } from '../../api/queries/trace'

// Ported from internal/web/templates/probe_history.html against
// GET /api/ui/trace/history (internal/api/probe.go).
export function ProbeHistoryPage() {
  const [params, setParams] = useSearchParams()
  const url = params.get('url') ?? ''
  const q = params.get('q') ?? ''
  const range = params.get('range') ?? '7'
  const actor = params.get('actor') ?? ''
  const outcome = params.get('outcome') ?? ''
  const probe = params.get('probe') ?? ''
  const cursor = params.get('cursor') ?? ''

  const { data, isPending, isError, error } = useProbeHistory({ url, q, range, actor, outcome, probe, cursor })

  const exportURL = `/api/ui/trace/history?export=csv&range=${range}${url ? `&url=${encodeURIComponent(url)}` : ''}${q ? `&q=${encodeURIComponent(q)}` : ''}${actor ? `&actor=${actor}` : ''}${outcome ? `&outcome=${outcome}` : ''}`

  return (
    <>
      <EuiFlexGroup alignItems="center" justifyContent="spaceBetween">
        <EuiFlexItem grow={false}><EuiTitle size="m"><h1>Probe history{url && ` — ${url}`}</h1></EuiTitle></EuiFlexItem>
        <EuiFlexItem grow={false}>
          <EuiFlexGroup gutterSize="s">
            {url && <EuiFlexItem grow={false}><EuiButton size="s" href={`/trace?url=${encodeURIComponent(url)}`}>Back to trace</EuiButton></EuiFlexItem>}
            {url && <EuiFlexItem grow={false}><EuiButton size="s" href="/trace/history">All probes</EuiButton></EuiFlexItem>}
            {data && data.probes.length > 0 && <EuiFlexItem grow={false}><EuiButton size="s" href={exportURL} iconType="download">Export audit</EuiButton></EuiFlexItem>}
          </EuiFlexGroup>
        </EuiFlexItem>
      </EuiFlexGroup>
      <EuiSpacer />

      <EuiFlexGroup gutterSize="s">
        <EuiFlexItem grow={false} style={{ width: 260 }}>
          <EuiFieldSearch placeholder="filter by entry point, actor or probe id" defaultValue={q}
            onSearch={(v) => setParams((p) => { p.set('q', v); p.delete('cursor'); return p })} />
        </EuiFlexItem>
        <EuiFlexItem grow={false}>
          <EuiSelect options={[{ value: '7', text: 'Range: 7 days' }, { value: '30', text: 'Range: 30 days' }, { value: '90', text: 'Range: 90 days' }, { value: 'all', text: 'Range: all' }]}
            value={range} onChange={(e) => setParams((p) => { p.set('range', e.target.value); p.delete('cursor'); return p })} />
        </EuiFlexItem>
        {data && data.actors.length > 0 && (
          <EuiFlexItem grow={false}>
            <EuiSelect options={[{ value: '', text: 'Actor: any' }, ...data.actors.map((a) => ({ value: a, text: a }))]}
              value={actor} onChange={(e) => setParams((p) => { p.set('actor', e.target.value); p.delete('cursor'); return p })} />
          </EuiFlexItem>
        )}
        <EuiFlexItem grow={false}>
          <EuiSelect options={[{ value: '', text: 'Outcome: any' }, { value: 'completed', text: 'Completed' }, { value: 'failed', text: 'Failed' }, { value: 'blocked', text: 'Blocked' }, { value: 'running', text: 'Running' }]}
            value={outcome} onChange={(e) => setParams((p) => { p.set('outcome', e.target.value); p.delete('cursor'); return p })} />
        </EuiFlexItem>
      </EuiFlexGroup>
      <EuiSpacer />

      {isPending && <EuiLoadingChart size="xl" />}
      {isError && <EuiText color="danger">{error.message}</EuiText>}

      {data && (
        <EuiFlexGroup alignItems="flexStart">
          <EuiFlexItem>
            <EuiPanel>
              <EuiText size="s" color="subdued">{data.total} probe(s){data.actorCount > 0 && ` · ${data.actorCount} actor(s)`}</EuiText>
              <EuiSpacer size="s" />
              {data.probes.length === 0 ? (
                <EuiText size="s" color="subdued">
                  {q || actor || outcome || range !== 'all'
                    ? <>No probe matches this filter. <a href={`/trace/history?range=all${url ? `&url=${encodeURIComponent(url)}` : ''}`}>Widen it to every probe on record</a>.</>
                    : url ? <>No probe has ever been sent at this entry point. <a href={`/trace?url=${encodeURIComponent(url)}`}>Trace it</a> and use Probe to send one.</>
                    : 'No probe has been sent yet.'}
                </EuiText>
              ) : (
                <table style={{ width: '100%', fontSize: 12 }}>
                  <thead><tr style={{ textAlign: 'left' }}><th>Ran</th><th>Entry point</th><th>Probe id</th><th>Status</th><th>State changes</th><th>Actor</th></tr></thead>
                  <tbody>
                    {data.probes.map((p) => {
                      const rowParams = new URLSearchParams()
                      if (url) rowParams.set('url', url)
                      if (q) rowParams.set('q', q)
                      rowParams.set('range', range)
                      if (actor) rowParams.set('actor', actor)
                      if (outcome) rowParams.set('outcome', outcome)
                      rowParams.set('probe', String(p.probeId))
                      return (
                      <tr key={p.probeId.toString()} style={probe === String(p.probeId) ? { background: 'rgba(0,119,204,0.08)' } : undefined}>
                        <td>{new Date(p.requestedAt).toLocaleString()}</td>
                        <td><a href={`?${rowParams.toString()}`} title={p.url}>{p.url}</a></td>
                        <td>{p.token.slice(0, 6)}</td>
                        <td>{p.status || '—'}</td>
                        <td>{p.changes.length > 0 ? p.changes.join('; ') : `none${p.result !== 'completed' ? ` · ${p.result}` : ''}`}</td>
                        <td>{p.actorLabel || '—'}</td>
                      </tr>
                      )
                    })}
                  </tbody>
                </table>
              )}
              {data.probes.length > 0 && (
                <>
                  <EuiSpacer size="s" />
                  <EuiFlexGroup alignItems="center">
                    <EuiFlexItem grow={false}><EuiText size="xs" color="subdued">{data.from}–{data.to} of {data.total}</EuiText></EuiFlexItem>
                    {data.hasMore && <EuiFlexItem grow={false}><EuiButton size="s" onClick={() => setParams((p) => { p.set('cursor', data.nextCursor); return p })}>Older ↓</EuiButton></EuiFlexItem>}
                  </EuiFlexGroup>
                </>
              )}
            </EuiPanel>
          </EuiFlexItem>

          <EuiFlexItem grow={false} style={{ width: 340 }}>
            {data.selected ? (
              <>
                <EuiPanel>
                  <EuiFlexGroup alignItems="center" gutterSize="s">
                    <EuiFlexItem><EuiText size="s"><strong>{data.selected.token.slice(0, 6)}</strong></EuiText></EuiFlexItem>
                    {data.selected.status ? <EuiBadge>{data.selected.status}</EuiBadge> : <EuiBadge color="default">no response</EuiBadge>}
                    <EuiFlexItem grow={false}><EuiButton size="s" href={`/trace?url=${encodeURIComponent(data.selected.url)}`}>Open trace</EuiButton></EuiFlexItem>
                  </EuiFlexGroup>
                  <EuiSpacer size="s" />
                  <EuiText size="xs">
                    <p>entry: {data.selected.url}</p>
                    <p>actor: {data.selected.actorLabel || '—'}</p>
                    <p>source: {data.selected.originHost || '—'}</p>
                    <p>method: {data.selected.method}{data.selected.redirectCount > 0 && ` · ${data.selected.redirectCount} redirect(s)`}</p>
                    {Number(data.selected.durationMs) > 0 && <p>latency: {Number(data.selected.durationMs)} ms</p>}
                    {data.selected.logReads && <p>log reads: {data.selected.logReads}</p>}
                    <p>requested: {new Date(data.selected.requestedAt).toLocaleString()}</p>
                  </EuiText>
                  {data.selected.error && <EuiText size="s" color="danger">{data.selected.error}</EuiText>}
                </EuiPanel>
                <EuiSpacer size="s" />
                <EuiPanel>
                  <EuiFlexGroup alignItems="center"><EuiFlexItem><EuiTitle size="xs"><h2>State changes</h2></EuiTitle></EuiFlexItem><EuiText size="xs" color="subdued">{data.selected.changeCount} change(s)</EuiText></EuiFlexGroup>
                  <EuiSpacer size="s" />
                  {data.selected.changes.length === 0 ? (
                    <EuiText size="s" color="subdued">This probe produced no evidence at any hop, so it raised nothing.</EuiText>
                  ) : (
                    data.selected.changes.map((c, i) => (
                      <EuiFlexGroup key={i} gutterSize="s" alignItems="center">
                        <EuiFlexItem><EuiText size="s">{c.host}</EuiText></EuiFlexItem>
                        <EuiBadge color="primary">{c.prior.toUpperCase()}</EuiBadge>
                        {c.to ? <><EuiText size="xs">→</EuiText><EuiBadge color={c.to === 'verified' ? 'success' : 'warning'}>{c.to.toUpperCase()}</EuiBadge></> : <EuiText size="xs" color="subdued">unchanged</EuiText>}
                      </EuiFlexGroup>
                    ))
                  )}
                </EuiPanel>
              </>
            ) : (
              <EuiPanel><EuiText size="s" color="subdued">Select a probe from the table to see its details and evidence.</EuiText></EuiPanel>
            )}
          </EuiFlexItem>
        </EuiFlexGroup>
      )}
    </>
  )
}
