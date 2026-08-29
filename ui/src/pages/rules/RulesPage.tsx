import {
  EuiAccordion,
  EuiButton,
  EuiCheckbox,
  EuiFieldText,
  EuiFlexGroup,
  EuiFlexItem,
  EuiLoadingChart,
  EuiPageTemplate,
  EuiPanel,
  EuiSpacer,
  EuiStat,
  EuiText,
  EuiTitle,
} from '@elastic/eui'
import { useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useRules } from '../../api/queries/rules'
import type { LookupResultPB, RuleGroup } from '../../api/pb/nagipath/api/v1/rules_pb'

// Ported from internal/web/templates/rules.html against GET /api/ui/rules
// (internal/api/rules.go): "which rules are in effect for this context path"
// without following the request anywhere — the same site/route selection
// the trace engine uses, stopped after one hop.
export function RulesPage() {
  const [params, setParams] = useSearchParams()
  const url = params.get('url') ?? ''
  const [urlInput, setUrlInput] = useState(url)
  const classes = params.getAll('class')
  const vendors = params.getAll('vendor')
  const page = Number(params.get('page') ?? '1')

  const { data, isPending, isError, error } = useRules({
    url, hostname: params.get('hostname') ?? '', path: params.get('path') ?? '',
    scheme: params.get('scheme') ?? '', port: params.get('port') ?? '', classes, vendors,
  })

  const submit = () => setParams((p) => { p.set('url', urlInput); p.delete('page'); return p })
  const toggle = (key: 'class' | 'vendor', value: string) => setParams((p) => {
    const cur = p.getAll(key)
    p.delete(key)
    for (const v of cur.includes(value) ? cur.filter((c) => c !== value) : [...cur, value]) p.append(key, v)
    p.delete('page')
    return p
  })

  const exportURL = `/api/ui/rules?export=csv${url ? `&url=${encodeURIComponent(url)}` : ''}${classes.map((c) => `&class=${c}`).join('')}${vendors.map((v) => `&vendor=${v}`).join('')}`

  return (
    <>
      <EuiFlexGroup alignItems="center" justifyContent="spaceBetween">
        <EuiFlexItem grow={false}><EuiTitle size="m"><h1>Rule lookup</h1></EuiTitle></EuiFlexItem>
        {data?.asked && <EuiFlexItem grow={false}><EuiButton href={exportURL} iconType="download">Export CSV</EuiButton></EuiFlexItem>}
      </EuiFlexGroup>
      <EuiSpacer />

      <EuiFlexGroup gutterSize="s">
        <EuiFlexItem>
          <EuiFieldText
            placeholder="https://shop.example.com/api/v2/charge or /api/v2"
            value={urlInput}
            onChange={(e) => setUrlInput(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && submit()}
          />
        </EuiFlexItem>
        <EuiFlexItem grow={false}><EuiButton fill onClick={submit}>Look up</EuiButton></EuiFlexItem>
      </EuiFlexGroup>
      <EuiSpacer />

      {isPending && <EuiLoadingChart size="xl" />}
      {isError && <EuiText color="danger">{error.message}</EuiText>}

      {data?.empty && (
        <EuiPageTemplate.EmptyPrompt title={<h2>Nothing is collected yet</h2>}
          body={<p>Rule lookup reads collected configuration, and there is none. <a href="/nodes/import">Import inventory</a> first.</p>} />
      )}

      {data && !data.empty && !data.asked && (
        <EuiPanel>
          <EuiText size="s"><strong>Paste a URL above.</strong> Look up the collected rules that would handle one
            request. A bare path, like <code>/api/v2</code>, searches every site in the fleet for a route that
            claims it, with no host needed.</EuiText>
        </EuiPanel>
      )}

      {data?.asked && (
        <>
          <EuiFlexGroup>
            <EuiFlexItem><EuiStat title={data.rules} description="Rules" titleSize="s" /></EuiFlexItem>
            <EuiFlexItem><EuiStat title={data.nodes} description="Nodes" titleSize="s" /></EuiFlexItem>
            <EuiFlexItem><EuiStat title={data.files} description="Files" titleSize="s" /></EuiFlexItem>
          </EuiFlexGroup>
          <EuiSpacer />

          <EuiFlexGroup>
            <EuiFlexItem grow={false} style={{ width: 220 }}>
              <EuiPanel>
                <EuiText size="xs"><strong>Action class</strong></EuiText>
                <EuiSpacer size="xs" />
                {data.classFacets.map((f) => (
                  <div key={f.value}>
                    <EuiCheckbox id={`class-${f.value}`} label={`${f.value} (${f.count})`}
                      checked={classes.includes(f.value)} onChange={() => toggle('class', f.value)} />
                  </div>
                ))}
                <EuiSpacer size="s" />
                <EuiText size="xs"><strong>Vendor</strong></EuiText>
                <EuiSpacer size="xs" />
                {data.vendorFacets.map((f) => (
                  <div key={f.value}>
                    <EuiCheckbox id={`vendor-${f.value}`} label={`${f.value} (${f.count})`}
                      checked={vendors.includes(f.value)} onChange={() => toggle('vendor', f.value)} />
                  </div>
                ))}
                <EuiSpacer size="s" />
                <EuiText size="xs" color="subdued">Nothing ticked means everything. Counts show how many rules each filter would leave.</EuiText>
              </EuiPanel>
            </EuiFlexItem>

            <EuiFlexItem>
              {data.groups.length > 0 ? (
                <>
                  <EuiText size="s" color="subdued">{data.from}–{data.to} node(s), page {data.page} of {data.pages}</EuiText>
                  <EuiSpacer size="s" />
                  {data.groups.map((g) => <RuleGroupPanel key={g.hash} group={g} />)}
                  {data.pages > 1 && (
                    <EuiFlexGroup gutterSize="xs" justifyContent="center">
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
              ) : data.rulesUnfiltered > 0 ? (
                <EuiPanel><EuiText size="s"><strong>No rules match the filters.</strong> {data.rulesUnfiltered} rule(s) were
                  found, but none match the action class or vendor filters on the left. Untick them to widen.</EuiText></EuiPanel>
              ) : (
                <EuiPanel><EuiText size="s"><strong>No instance claims that context path.</strong> Every collected
                  instance was checked. See below for why.</EuiText></EuiPanel>
              )}

              {data.silent.length > 0 && (
                <>
                  <EuiSpacer />
                  <EuiAccordion id="silent" buttonContent={`${data.silent.length} instance(s) with no candidates`}>
                    <EuiSpacer size="s" />
                    {data.silent.map((res) => (
                      <EuiFlexGroup key={res.instId.toString()} gutterSize="s" style={{ padding: '4px 0' }}>
                        <EuiFlexItem grow={false} style={{ width: 200 }}>
                          <a href={`/instances/${res.instId}`}>{res.instDisplayName}</a>
                        </EuiFlexItem>
                        <EuiFlexItem grow={false} style={{ width: 100 }}><EuiText size="s" color="subdued">{res.instVendor}</EuiText></EuiFlexItem>
                        <EuiFlexItem><EuiText size="s">{res.reason || 'no rules matched'}</EuiText></EuiFlexItem>
                      </EuiFlexGroup>
                    ))}
                  </EuiAccordion>
                </>
              )}
            </EuiFlexItem>
          </EuiFlexGroup>
        </>
      )}
    </>
  )
}

function RuleGroupPanel({ group }: { group: RuleGroup }) {
  const res: LookupResultPB | undefined = group.results[0]
  return (
    <EuiPanel style={{ marginBottom: 10 }}>
      <EuiFlexGroup gutterSize="s" alignItems="center">
        <EuiFlexItem grow={false}><EuiText size="s"><strong>{res?.instNodeName || '(unknown node)'}</strong></EuiText></EuiFlexItem>
        <EuiFlexItem grow={false}><EuiText size="s" color="subdued">{res?.instVendor}</EuiText></EuiFlexItem>
        {group.count > 1 && (
          <EuiFlexItem grow={false}><EuiText size="xs" color="subdued">· {group.count} nodes with identical rules</EuiText></EuiFlexItem>
        )}
      </EuiFlexGroup>
      {res?.siteName && <EuiText size="xs" color="subdued">site: {res.siteName}{res.matchedBy && ` · ${res.matchedBy}`}</EuiText>}
      {res?.routePattern && <EuiText size="xs" color="subdued">route: {res.routePattern}</EuiText>}
      <EuiSpacer size="s" />
      <table style={{ width: '100%', fontSize: 12 }}>
        <thead>
          <tr style={{ textAlign: 'left' }}><th>#</th><th>Directive</th><th>Arguments</th><th>Class</th></tr>
        </thead>
        <tbody>
          {(res?.rules ?? []).filter((lr) => !lr.inherited).map((lr, i) => (
            <tr key={i} style={lr.shadowed ? { opacity: 0.5 } : undefined}>
              <td>{lr.shadowed ? '—' : lr.ordinal}</td>
              <td>{lr.directive}</td>
              <td>{lr.args}{lr.shadowed && <div style={{ fontSize: 10 }}>{lr.shadowedBy}</div>}</td>
              <td>{lr.actionClass}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </EuiPanel>
  )
}
