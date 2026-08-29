import {
  EuiBadge,
  EuiButton,
  EuiCheckbox,
  EuiFieldText,
  EuiFlexGroup,
  EuiFlexItem,
  EuiLoadingChart,
  EuiPageTemplate,
  EuiPanel,
  EuiSelect,
  EuiSpacer,
  EuiStat,
  EuiText,
  EuiTitle,
} from '@elastic/eui'
import { useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useSearch } from '../../api/queries/search'

// Ported from internal/web/templates/search.html against GET /api/ui/search
// (internal/api/search.go): a structured rule search plus a raw config-text
// search, grouped by (instance, file).
export function SearchPage() {
  const [params, setParams] = useSearchParams()
  const q = params.get('q') ?? ''
  const [qInput, setQInput] = useState(q)
  const match = params.get('match') ?? 'substring'
  const scope = params.get('scope') ?? 'current'
  const page = Number(params.get('page') ?? '1')
  const vendor = params.getAll('vendor')
  const file = params.getAll('file')
  const cluster = params.getAll('cluster')
  const age = params.getAll('age')

  const { data, isPending, isError, error } = useSearch({ q, match, scope, page, vendor, file, cluster, age })

  const submit = () => setParams((p) => { p.set('q', qInput); p.delete('page'); return p })
  const toggle = (key: 'vendor' | 'file' | 'cluster' | 'age', value: string) => setParams((p) => {
    const cur = p.getAll(key)
    p.delete(key)
    for (const v of cur.includes(value) ? cur.filter((c) => c !== value) : [...cur, value]) p.append(key, v)
    p.delete('page')
    return p
  })

  const anyFilter = vendor.length > 0 || file.length > 0 || cluster.length > 0 || age.length > 0

  return (
    <>
      <EuiTitle size="m"><h1>Config search</h1></EuiTitle>
      <EuiSpacer />

      <EuiFlexGroup gutterSize="s">
        <EuiFlexItem>
          <EuiFieldText placeholder="proxy_pass, X-Frame-Options, 10.90.4.*" value={qInput}
            onChange={(e) => setQInput(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && submit()} />
        </EuiFlexItem>
        <EuiFlexItem grow={false}>
          <EuiSelect options={[{ value: 'substring', text: 'Match: substring' }, { value: 'regex', text: 'Match: regex' }]}
            value={match} onChange={(e) => setParams((p) => { p.set('match', e.target.value); return p })} />
        </EuiFlexItem>
        <EuiFlexItem grow={false}>
          <EuiSelect options={[{ value: 'current', text: 'Scope: current config' }, { value: 'all', text: 'Scope: all snapshots' }]}
            value={scope} onChange={(e) => setParams((p) => { p.set('scope', e.target.value); return p })} />
        </EuiFlexItem>
        <EuiFlexItem grow={false}><EuiButton fill onClick={submit}>Search</EuiButton></EuiFlexItem>
      </EuiFlexGroup>
      <EuiSpacer />

      {isPending && q && <EuiLoadingChart size="xl" />}
      {isError && <EuiText color="danger">{error.message}</EuiText>}

      {!q && (
        <EuiPageTemplate.EmptyPrompt title={<h2>Search configuration text</h2>}
          body={<p>Searches across every current snapshot. Modelled directives — what nagipath parsed and understood
            — and the verbatim text of every configuration file. A directive nagipath does not model is still in the
            text index, so a miss means the words are genuinely absent.</p>} />
      )}

      {data?.regexError && (
        <EuiPageTemplate.EmptyPrompt iconType="alert" color="danger" title={<h2>Invalid regular expression</h2>}
          body={<p>{data.regexError}</p>} />
      )}

      {data?.error && <EuiText color="danger">{data.error}</EuiText>}

      {data && q && !data.regexError && (
        <>
          <EuiFlexGroup>
            <EuiFlexItem><EuiStat title={data.matches} description="Matches" titleSize="s" /></EuiFlexItem>
            <EuiFlexItem><EuiStat title={data.filesHit} description="Files" titleSize="s" /></EuiFlexItem>
            <EuiFlexItem><EuiStat title={data.nodesHit} description={`Nodes · ${data.duration}`} titleSize="s" /></EuiFlexItem>
          </EuiFlexGroup>
          <EuiSpacer />

          <EuiFlexGroup>
            <EuiFlexItem grow={false} style={{ width: 220 }}>
              <EuiPanel>
                {[
                  { label: 'Vendor', key: 'vendor' as const, facets: data.vendors, sel: vendor },
                  { label: 'Cluster', key: 'cluster' as const, facets: data.clusters, sel: cluster },
                  ...(data.snapshotAge.length ? [{ label: 'Snapshot age', key: 'age' as const, facets: data.snapshotAge, sel: age }] : []),
                  { label: 'File', key: 'file' as const, facets: data.files, sel: file },
                ].map(({ label, key, facets, sel }) => (
                  <div key={key}>
                    <EuiText size="xs"><strong>{label}</strong></EuiText>
                    <EuiSpacer size="xs" />
                    {facets.map((f) => (
                      <div key={f.value}>
                        <EuiCheckbox id={`${key}-${f.value}`} label={`${f.value} (${f.count})`}
                          checked={sel.includes(f.value)} onChange={() => toggle(key, f.value)} />
                      </div>
                    ))}
                    <EuiSpacer size="s" />
                  </div>
                ))}
                <EuiText size="xs" color="subdued">Nothing ticked means everything. Counts are over the whole result set, so narrowing is always reversible.</EuiText>
              </EuiPanel>
            </EuiFlexItem>

            <EuiFlexItem>
              {data.groups.length === 0 ? (
                <EuiPanel>
                  <EuiText size="s"><strong>No matches.</strong> Nothing in the current configuration of any
                    instance matched <code>{q}</code>{anyFilter && ' under the filters on the left — untick them to widen'}.</EuiText>
                </EuiPanel>
              ) : (
                <>
                  {data.groups.map((g) => (
                    <EuiPanel key={`${g.instanceId}:${g.fileId}`} style={{ marginBottom: 10 }}>
                      <EuiFlexGroup gutterSize="s" alignItems="center">
                        <EuiFlexItem grow={false}><EuiText size="s"><strong>{g.instance}</strong></EuiText></EuiFlexItem>
                        {g.node && <EuiFlexItem grow={false}><EuiText size="s" color="subdued">on {g.node}</EuiText></EuiFlexItem>}
                        <EuiFlexItem><EuiText size="s" color="subdued">{g.path}</EuiText></EuiFlexItem>
                        <EuiFlexItem grow={false}>
                          <EuiButton size="s" href={`/snapshots/${g.snapshotId}/file/${g.fileId}`}>Open</EuiButton>
                        </EuiFlexItem>
                      </EuiFlexGroup>
                      <EuiSpacer size="s" />
                      {g.rules.length > 0 && (
                        <table style={{ width: '100%', fontSize: 12 }}>
                          <thead><tr style={{ textAlign: 'left' }}><th>Directive</th><th>Arguments</th><th>Class</th><th>At</th></tr></thead>
                          <tbody>
                            {g.rules.map((rh, i) => (
                              <tr key={i} style={rh.shadowed ? { opacity: 0.5 } : undefined}>
                                <td>{rh.directive}{rh.shadowed && <EuiBadge color="danger" style={{ marginLeft: 6 }}>shadowed</EuiBadge>}</td>
                                <td>{rh.args}</td>
                                <td>{rh.actionClass}</td>
                                <td><a href={`/snapshots/${rh.snapshotId}/file/${rh.fileId}?b=${rh.byteStart}`}>@{rh.byteStart}</a></td>
                              </tr>
                            ))}
                          </tbody>
                        </table>
                      )}
                      {g.texts.length > 0 && (
                        <div style={{ fontFamily: 'monospace', fontSize: 12, marginTop: 8 }}>
                          {g.texts.map((th, i) => (
                            <div key={i} style={{ display: 'flex', gap: 8 }}>
                              <span title="matched the file text, not a directive nagipath models" style={{ color: '#888' }}>text</span>
                              <span>{th.snippet}</span>
                            </div>
                          ))}
                        </div>
                      )}
                    </EuiPanel>
                  ))}

                  {data.instances > 0 && (
                    <EuiFlexGroup gutterSize="xs" alignItems="center">
                      <EuiFlexItem grow={false}><EuiText size="s" color="subdued">{data.from}–{data.to} of {data.instances} instance(s)</EuiText></EuiFlexItem>
                      <EuiFlexItem />
                      <EuiFlexItem grow={false}>
                        <EuiButton size="s" isDisabled={page <= 1} onClick={() => setParams((p) => { p.set('page', String(page - 1)); return p })}>‹</EuiButton>
                      </EuiFlexItem>
                      <EuiFlexItem grow={false}><EuiText size="s">{data.page} / {data.pages}</EuiText></EuiFlexItem>
                      <EuiFlexItem grow={false}>
                        <EuiButton size="s" isDisabled={page >= data.pages} onClick={() => setParams((p) => { p.set('page', String(page + 1)); return p })}>›</EuiButton>
                      </EuiFlexItem>
                    </EuiFlexGroup>
                  )}
                </>
              )}
            </EuiFlexItem>
          </EuiFlexGroup>
        </>
      )}
    </>
  )
}
