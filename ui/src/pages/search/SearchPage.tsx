import { useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useSearch } from '../../api/queries/search'
import type { SearchGroup } from '../../api/pb/nagipath/api/v1/search_pb'
import { PageHeader } from '../../components/shared/PageHeader'
import { PanelHeader } from '../../components/shared/PanelHeader'
import {
  Badge, Button, Checkbox, EmptyPrompt, Field, Loading, Panel, PanelFooter,
  Select,
} from '../../components/ui'

type FacetKey = 'vendor' | 'file' | 'cluster' | 'age'

// design/'s screen 2g match card: one (node, file) pair, its matched lines in a
// .code block, and a link into the stored file.
// The index returns matched fragments and byte offsets, not file windows with
// line numbers, so the gutter carries the byte offset of each rule hit and
// there are no surrounding context lines — "Open" is what shows the file.
function MatchCard({ g }: { g: SearchGroup }) {
  const count = g.rules.length + g.texts.length
  return (
    <div style={{ border: '1px solid #d3dae6', borderRadius: 5, overflow: 'hidden' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '6px 9px', background: '#f7f8fc', borderBottom: '1px solid #d3dae6' }}>
        <span className="m" style={{ fontWeight: 600 }}>{g.node || g.instance}</span>
        <span className="m mu" style={{ flex: 1, minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis' }}>{g.path}</span>
        <span className="m mus">{count} match{count === 1 ? '' : 'es'}</span>
        <Button small subtle href={`/snapshots/${g.snapshotId}/file/${g.fileId}`}>Open</Button>
      </div>
      <div className="code" style={{ border: 0, borderRadius: 0 }}>
        {g.rules.map((rh) => (
          <div className="cl on" key={`r${rh.ruleId}`} style={{ whiteSpace: 'pre-wrap' }}>
            <a className="no" style={{ width: 46 }} href={`/snapshots/${rh.snapshotId}/file/${rh.fileId}?b=${rh.byteStart}`}>{rh.byteStart}</a>
            <span style={{ flex: 1, minWidth: 0 }}>
              <span className="k">{rh.directive}</span> {rh.args}
              {rh.shadowed && <span style={{ marginLeft: 6 }}><Badge cls="i">SHADOWED</Badge></span>}
              <span className="c"> · {rh.actionClass}</span>
            </span>
          </div>
        ))}
        {g.texts.map((th, i) => (
          <div className="cl" key={`t${i}`} style={{ whiteSpace: 'pre-wrap' }}>
            <span className="no" style={{ width: 46 }} title="matched the file text, not a directive nagipath models">text</span>
            <span style={{ flex: 1, minWidth: 0 }}>{th.snippet}</span>
          </div>
        ))}
      </div>
    </div>
  )
}

// Ported from internal/web/templates/search.html against GET /api/search
// (internal/api/search.go): a structured rule search plus a raw config-text
// search, grouped by (instance, file). Laid out as design/'s screen 2g: the
// counts row with removable filter chips, the 216px facet rail, and one bordered
// card per file whose matched lines sit in a .code block.
// Omitted from that screen: "Recent · 12"/"Save search" (no saved-search store),
// the "Group: file" select (grouping is always by file — SearchResponse has no
// other grouping) and the "Context: 1 line" select with the design's
// surrounding un-matched lines (SearchConfigText returns an FTS5 snippet and
// SearchRules a byte offset; neither carries a line number or a file window, and
// reading each hit's blob back to build one would be a blob read per hit), and
// the design's literal "case-insensitive"/"exclude commented lines" chips (the
// FTS index is already case-insensitive and does not distinguish comments — the
// chips shown instead are the filters actually in force).
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
  const toggle = (key: FacetKey, value: string) => setParams((p) => {
    const cur = p.getAll(key)
    p.delete(key)
    for (const v of cur.includes(value) ? cur.filter((c) => c !== value) : [...cur, value]) p.append(key, v)
    p.delete('page')
    return p
  })
  const goto = (n: number) => setParams((p) => { p.set('page', String(n)); return p })

  const rails: { label: string; key: FacetKey; facets: { value: string; count: number }[]; sel: string[] }[] = [
    { label: 'Vendor', key: 'vendor', facets: data?.vendors ?? [], sel: vendor },
    { label: 'File', key: 'file', facets: data?.files ?? [], sel: file },
    { label: 'Cluster', key: 'cluster', facets: data?.clusters ?? [], sel: cluster },
    { label: 'Snapshot age', key: 'age', facets: data?.snapshotAge ?? [], sel: age },
  ]
  const active = rails.flatMap(({ key, sel }) => sel.map((v) => ({ key, v })))

  return (
    <>
      <PageHeader title="Config search" />

      <div className="qbar">
        <Field
          placeholder="proxy_pass, X-Frame-Options, 10.90.4.*"
          value={qInput}
          onChange={setQInput}
          onEnter={submit}
          grow
        />
        <Select
          options={[{ value: 'substring', text: 'Match: substring' }, { value: 'regex', text: 'Match: regex' }]}
          value={match}
          onChange={(v) => setParams((p) => { p.set('match', v); return p })}
        />
        <Select
          options={[{ value: 'current', text: 'Scope: current config' }, { value: 'all', text: 'Scope: all snapshots' }]}
          value={scope}
          onChange={(v) => setParams((p) => { p.set('scope', v); return p })}
        />
        <Button primary onClick={submit}>Search</Button>
      </div>

      {data && q && !data.regexError && (
        <div className="row" style={{ padding: '7px 16px 0', alignItems: 'center', flex: 'none' }}>
          <span className="m mu">
            {data.matches} match{data.matches === 1 ? '' : 'es'} · {data.filesHit} file{data.filesHit === 1 ? '' : 's'} ·{' '}
            {data.nodesHit} node{data.nodesHit === 1 ? '' : 's'}{data.duration && ` · ${data.duration}`}
          </span>
          <div style={{ flex: 1 }} />
          {active.map(({ key, v }) => (
            <span className="chip" key={`${key}-${v}`} onClick={() => toggle(key, v)} style={{ cursor: 'pointer' }}>
              {v} <span className="x">✕</span>
            </span>
          ))}
        </div>
      )}

      <div className="bd">
        {isPending && q && <Loading label="Searching…" />}
        {isError && <EmptyPrompt danger title="Could not search" body={error.message} />}

        {!q && (
          <EmptyPrompt
            title="Search configuration text"
            body={
              <>
                Searches across every current snapshot. Modelled directives — what nagipath parsed and understood —
                and the verbatim text of every configuration file. A directive nagipath does not model is still in
                the text index, so a miss means the words are genuinely absent.
              </>
            }
          />
        )}

        {data?.regexError && (
          <EmptyPrompt danger title="Invalid regular expression" body={data.regexError} />
        )}

        {data?.error && <div className="m" style={{ color: '#a1231c' }}>{data.error}</div>}

        {data && q && !data.regexError && (
          <div className="row" style={{ flex: 1, minHeight: 0, alignItems: 'stretch' }}>
            <div className="col" style={{ flex: '0 0 216px', minWidth: 0 }}>
              <Panel style={{ padding: 10, flex: 1, minHeight: 0, overflow: 'auto' }}>
                {rails.filter((r) => r.facets.length > 0).map(({ label, key, facets, sel }, i) => (
                  <div key={key}>
                    <div className="lbl" style={{ margin: i === 0 ? '0 0 6px' : '10px 0 6px' }}>{label}</div>
                    {facets.map((f) => (
                      <div className="fct" key={f.value}>
                        <Checkbox id={`${key}-${f.value}`} checked={sel.includes(f.value)}
                          onChange={() => toggle(key, f.value)} label={f.value} />
                        <span className="c">{f.count}</span>
                      </div>
                    ))}
                  </div>
                ))}
                <div className="m mus" style={{ marginTop: 10 }}>
                  Nothing ticked means everything. Counts are over the whole result set, so narrowing is always
                  reversible.
                </div>
              </Panel>
            </div>

            <div className="col" style={{ flex: 1, minWidth: 0 }}>
              <Panel z style={{ flex: 1, minHeight: 0 }}>
                <PanelHeader title="Matches" meta="grouped by file · one card per node and file" />
                {data.groups.length === 0 ? (
                  <div className="m mu" style={{ padding: '10px 12px' }}>
                    Nothing in the current configuration of any instance matched <span className="m">{q}</span>
                    {active.length > 0 && ' under the filters in force — remove a chip above to widen'}.
                  </div>
                ) : (
                  <div style={{ flex: 1, minHeight: 0, overflow: 'auto', padding: '10px 12px', display: 'flex', flexDirection: 'column', gap: 9 }}>
                    {data.groups.map((g) => <MatchCard key={`${g.instanceId}:${g.fileId}`} g={g} />)}
                  </div>
                )}
                <PanelFooter>
                  <span className="m mus">{data.groups.length} of {data.filesHit} file{data.filesHit === 1 ? '' : 's'}</span>
                  <div style={{ flex: 1 }} />
                  {data.pages > 1 && (
                    <span className="pg">
                      {data.from}–{data.to} of {data.instances}
                      <span className="pgb" onClick={() => page > 1 && goto(page - 1)}>‹</span>
                      {Array.from({ length: data.pages }, (_, i) => i + 1).map((n) => (
                        <span key={n} className={`pgb${n === data.page ? ' on' : ''}`} onClick={() => goto(n)}>{n}</span>
                      ))}
                      <span className="pgb" onClick={() => page < data.pages && goto(page + 1)}>›</span>
                    </span>
                  )}
                </PanelFooter>
              </Panel>
            </div>
          </div>
        )}
      </div>
    </>
  )
}
