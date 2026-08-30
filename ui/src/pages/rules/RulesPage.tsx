import { useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useRules } from '../../api/queries/rules'
import type { LookupRulePB, RuleGroup } from '../../api/pb/nagipath/api/v1/rules_pb'
import { PageHeader } from '../../components/shared/PageHeader'
import { PanelHeader } from '../../components/shared/PanelHeader'
import {
  Accordion, Badge, Button, Checkbox, EmptyPrompt, Loading, Panel,
  PanelFooter, Select,
} from '../../components/ui'

function baseName(path: string): string {
  return path.slice(path.lastIndexOf('/') + 1) || path
}

// One rule row of design/'s screen 2b. The design's two prose columns are
// filled from what the parser actually records: the scope the rule sits in
// ("when the request matches") and the directive itself ("what happens"). No
// plain-language template table exists, so the raw directive is the sub-line in
// both columns rather than a rewritten sentence.
function RuleRow({ lr, cols }: { lr: LookupRulePB; cols: number }) {
  const dim = lr.shadowed ? { color: '#98a2b3' } : undefined
  return (
    <tr style={dim}>
      <td className="m mus">{lr.shadowed ? '—' : lr.ordinal}</td>
      <td>
        <span className="m">{lr.scope || 'any request to this site'}</span>
      </td>
      <td>
        <span className="m" style={lr.shadowed ? undefined : { fontWeight: 500 }}>{lr.directive} {lr.args}</span>
        {lr.shadowed && lr.shadowedBy && <div className="m mus">{lr.shadowedBy}</div>}
      </td>
      <td className={lr.shadowed ? 'm mus' : 'm mu'}>{lr.actionClass}</td>
      <td className="m mus">
        {/* The parser stores the byte offset, not the line number, so the cell
            names the file and links to that exact byte in the stored copy. */}
        {Number(lr.fileId) > 0
          ? <a href={`/snapshots/${lr.snapshotId}/file/${lr.fileId}?b=${lr.byteStart}`} onClick={(e) => e.stopPropagation()}>{baseName(lr.path)}</a>
          : baseName(lr.path) || '—'}
      </td>
      <td>{lr.shadowed ? <Badge cls="i">SHADOWED</Badge> : <Badge cls="v">FIRES</Badge>}</td>
      {cols > 6 && <td />}
    </tr>
  )
}

// design/'s group band: one node's answer, with the identical-configuration
// siblings folded into it. Collapsed bands are click-to-open.
function GroupBody({ group, firesOnly }: { group: RuleGroup; firesOnly: boolean }) {
  const res = group.results[0]
  const [open, setOpen] = useState(true)
  const applies = (res?.rules ?? []).filter((lr) => !lr.inherited)
  const rules = firesOnly ? applies.filter((lr) => !lr.shadowed) : applies
  const siblings = group.count - 1

  return (
    <tbody>
      <tr onClick={() => setOpen(!open)} style={{ cursor: 'pointer' }}>
        <td colSpan={6} style={{ background: '#f7f8fc', borderBottom: '1px solid #d3dae6', borderTop: '1px solid #d3dae6' }}>
          <span className="m" style={{ fontWeight: 600 }}>{open ? '▾' : '▸'} {res?.instNodeName || '(unknown node)'}</span>{' '}
          <span className="m mus">
            {[res?.instVendor, res?.clusterName, res?.siteName && `site ${res.siteName}`, res?.routePattern]
              .filter(Boolean).join(' · ')}
            {' · '}{rules.length} of {res?.rules.length ?? 0} apply
          </span>
          {siblings > 0 && (
            <span className="m mus" style={{ marginLeft: 8 }}>— identical on {siblings} more node{siblings === 1 ? '' : 's'}</span>
          )}
          {res?.degraded && <span style={{ marginLeft: 8 }}><Badge cls="d">DEGRADED</Badge></span>}
        </td>
      </tr>
      {open && rules.map((lr, i) => <RuleRow key={`${lr.ordinal}-${lr.byteStart}-${i}`} lr={lr} cols={6} />)}
      {open && rules.length === 0 && (
        <tr><td colSpan={6} className="m mus">Every rule on this node is shadowed by an earlier one.</td></tr>
      )}
    </tbody>
  )
}

// Ported from internal/web/templates/rules.html against GET /api/rules
// (internal/api/rules.go): "which rules are in effect for this context path"
// without following the request anywhere — the same site/route selection the
// trace engine uses, stopped after one hop. Laid out as design/'s screen 2b:
// the host/path pair in the query bar, the 216px facet rail, and one grouped
// table where each band is a node and shadowed rules are greyed in place.
// Omitted from that screen: "Saved queries · 6"/"Save query" and "Advanced
// query" (there is no saved-query store and no query grammar behind the two
// fields — hostname and path are the whole request), the "Cluster: all" select
// (GetRules filters by action class and vendor only; cluster is shown on each
// band but not filterable server-side, and the facet counts come from the
// server), the "Plain language" reading select (the plain-language templates
// design/ assumes per directive type do not exist), the "Group: node"/"50 per
// page" selects (grouping is always by node and the page size is fixed
// server-side), the row checkboxes with "Compare"/"Trace each" (no compare
// endpoint, and tracing needs a URL per node rather than a bulk action), and
// the elapsed-time figure in the counts row (GetRules does not time itself).
export function RulesPage() {
  const [params, setParams] = useSearchParams()
  const hostname = params.get('hostname') ?? ''
  const path = params.get('path') ?? ''
  const [hostInput, setHostInput] = useState(hostname)
  const [pathInput, setPathInput] = useState(path)
  const classes = params.getAll('class')
  const vendors = params.getAll('vendor')
  const page = Number(params.get('page') ?? '1')
  const [firesOnly, setFiresOnly] = useState(false)

  const { data, isPending, isError, error } = useRules({
    url: params.get('url') ?? '', hostname, path,
    scheme: params.get('scheme') ?? '', port: params.get('port') ?? '',
    classes, vendors, page: params.get('page') ?? '',
  })

  const submit = () => setParams((p) => {
    p.delete('url')
    if (hostInput) p.set('hostname', hostInput); else p.delete('hostname')
    if (pathInput) p.set('path', pathInput); else p.delete('path')
    p.delete('page')
    return p
  })
  const toggle = (key: 'class' | 'vendor', value: string) => setParams((p) => {
    const cur = p.getAll(key)
    p.delete(key)
    for (const v of cur.includes(value) ? cur.filter((c) => c !== value) : [...cur, value]) p.append(key, v)
    p.delete('page')
    return p
  })
  const goto = (n: number) => setParams((p) => { p.set('page', String(n)); return p })

  const query = [hostname && `hostname=${encodeURIComponent(hostname)}`, path && `path=${encodeURIComponent(path)}`,
    ...classes.map((c) => `class=${c}`), ...vendors.map((v) => `vendor=${v}`)].filter(Boolean).join('&')

  return (
    <>
      <PageHeader
        title="Rule lookup"
        actions={data?.asked && <Button small href={`/api/rules?export=csv&${query}`}>Export CSV</Button>}
      />

      <div className="qbar">
        <span className="m mus">Which rules apply to</span>
        <span className="fld" style={{ flex: 1.4 }}>
          <span className="m mus">host</span>
          <input value={hostInput} onChange={(e) => setHostInput(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && submit()} placeholder="payments.corp.example" />
        </span>
        <span className="fld" style={{ flex: 1 }}>
          <span className="m mus">path</span>
          <input value={pathInput} onChange={(e) => setPathInput(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && submit()} placeholder="/api/v2/charge" />
        </span>
        <Button primary onClick={submit}>Find rules</Button>
      </div>

      {data?.asked && (
        <div className="row" style={{ padding: '7px 16px 0', alignItems: 'center', flex: 'none' }}>
          <span className="m mu">
            {data.rules} rule{data.rules === 1 ? '' : 's'} apply · {data.nodes} node{data.nodes === 1 ? '' : 's'} · {data.files} file{data.files === 1 ? '' : 's'}
          </span>
          <div style={{ flex: 1 }} />
          <span className="m mus">reading</span>
          <Select
            options={[{ value: '', text: 'All rules that apply' }, { value: '1', text: 'Only rules that fire' }]}
            value={firesOnly ? '1' : ''}
            onChange={(v) => setFiresOnly(v === '1')}
          />
        </div>
      )}

      <div className="bd">
        {isPending && <Loading label="Looking up rules…" />}
        {isError && <EmptyPrompt danger title="Could not look up rules" body={error.message} />}

        {data?.empty && (
          <EmptyPrompt
            title="Nothing is collected yet"
            body={<>Rule lookup reads collected configuration, and there is none. <a href="/nodes/import">Import inventory</a> first.</>}
          />
        )}

        {data && !data.empty && !data.asked && (
          <EmptyPrompt
            title="Name a host, a path, or both"
            body={<>Look up the collected rules that would handle one request. A bare path, like <span className="m">/api/v2</span>, searches every site in the fleet for a route that claims it, with no host needed.</>}
          />
        )}

        {data?.asked && (
          <div className="row" style={{ flex: 1, minHeight: 0, alignItems: 'stretch' }}>
            <div className="col" style={{ flex: '0 0 216px', minWidth: 0 }}>
              <Panel style={{ padding: 10, flex: 1, minHeight: 0, overflow: 'auto' }}>
                <div className="lbl" style={{ marginBottom: 6 }}>Vendor</div>
                {data.vendorFacets.map((f) => (
                  <div className="fct" key={f.value}>
                    <Checkbox id={`vendor-${f.value}`} checked={vendors.includes(f.value)}
                      onChange={() => toggle('vendor', f.value)} label={f.value} />
                    <span className="c">{f.count}</span>
                  </div>
                ))}
                <div className="lbl" style={{ margin: '10px 0 6px' }}>Action class</div>
                {data.classFacets.map((f) => (
                  <div className="fct" key={f.value}>
                    <Checkbox id={`class-${f.value}`} checked={classes.includes(f.value)}
                      onChange={() => toggle('class', f.value)} label={f.value} />
                    <span className="c">{f.count}</span>
                  </div>
                ))}
                <div className="m mus" style={{ marginTop: 10 }}>
                  Nothing ticked means everything. Counts show how many rules each filter would leave.
                </div>
              </Panel>
            </div>

            <div className="col" style={{ flex: 1, minWidth: 0 }}>
              <Panel z style={{ flex: 1, minHeight: 0 }}>
                <PanelHeader title="Rules that apply" meta="first match wins per node — rules below it are shown greyed" />
                {data.groups.length > 0 ? (
                  <div style={{ flex: 1, minHeight: 0, overflow: 'auto' }}>
                    <table className="t">
                      <thead>
                        <tr>
                          <th style={{ width: 30 }}>#</th>
                          <th style={{ width: 230 }}>When the request matches</th>
                          <th>What happens</th>
                          <th style={{ width: 86 }}>Kind</th>
                          <th style={{ width: 150 }}>Defined in</th>
                          <th style={{ width: 86 }}>State</th>
                        </tr>
                      </thead>
                      {data.groups.map((g) => <GroupBody key={g.hash} group={g} firesOnly={firesOnly} />)}
                    </table>
                  </div>
                ) : data.rulesUnfiltered > 0 ? (
                  <div style={{ padding: '10px 12px' }} className="m mu">
                    {data.rulesUnfiltered} rule(s) were found, but none match the action class or vendor filters on
                    the left. Untick them to widen.
                  </div>
                ) : (
                  <div style={{ padding: '10px 12px' }} className="m mu">
                    No instance claims that context path. Every collected instance was checked — see the list below
                    for why.
                  </div>
                )}
                <PanelFooter>
                  <span className="m mus">{data.from}–{data.to} of {data.nodes} node{data.nodes === 1 ? '' : 's'}</span>
                  <div style={{ flex: 1 }} />
                  {data.pages > 1 && (
                    <span className="pg">
                      <span className="pgb" onClick={() => page > 1 && goto(page - 1)}>‹</span>
                      {Array.from({ length: data.pages }, (_, i) => i + 1).map((n) => (
                        <span key={n} className={`pgb${n === data.page ? ' on' : ''}`} onClick={() => goto(n)}>{n}</span>
                      ))}
                      <span className="pgb" onClick={() => page < data.pages && goto(page + 1)}>›</span>
                    </span>
                  )}
                </PanelFooter>
              </Panel>

              {data.silent.length > 0 && (
                <Accordion title={`${data.silent.length} instance(s) with no candidates`}>
                  <div className="col">
                    {data.silent.map((res) => (
                      <div className="row" key={res.instId.toString()} style={{ alignItems: 'center' }}>
                        <div style={{ flex: '0 0 200px', minWidth: 0 }}>
                          <a className="m" href={`/nodes/${res.nodeId}?process=${res.instId}`}>{res.instDisplayName}</a>
                        </div>
                        <div className="m mu" style={{ flex: '0 0 100px' }}>{res.instVendor}</div>
                        <div className="m mus" style={{ flex: 1, minWidth: 0 }}>{res.reason || 'no rules matched'}</div>
                      </div>
                    ))}
                  </div>
                </Accordion>
              )}
            </div>
          </div>
        )}
      </div>
    </>
  )
}
