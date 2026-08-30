import { useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useSites } from '../../api/queries/sites'
import type { SiteListRow, SiteVariant } from '../../api/pb/nagipath/api/v1/sites_pb'
import { PageHeader } from '../../components/shared/PageHeader'
import { PanelHeader } from '../../components/shared/PanelHeader'
import { StatRow } from '../../components/shared/StatTile'
import {
  Badge, Button, Disclosure, EmptyPrompt, Facet, Kv, Loading, Panel,
  PanelFooter, QueryBar, Select, Table, type Column,
} from '../../components/ui'

// design/'s screen 3a detail, expanded under the hostname's own row: its
// aliases and listener beside its config variants side by side.
// GET /sites?site=<name> has always returned this (siteservice.go's `variants`,
// from store.SiteListWithVariants) — the per-variant route list just wasn't on
// the wire until now (sites.proto's SiteVariantRoute).
function VariantDetail({ row, variants }: { row: SiteListRow; variants: SiteVariant[] }) {
  return (
    <div className="row" style={{ alignItems: 'flex-start' }}>
      <div className="col" style={{ flex: '0 0 300px', gap: 6 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <span className="lbl">{row.name}</span>
          {row.variants > 1 && <Badge cls="d">{row.variants} VARIANTS</Badge>}
        </div>
        <Kv
          labelWidth={84}
          rows={[
            ['aliases', row.aliases.length > 0 ? row.aliases.join(', ') : <span className="mus">none</span>],
            ['listener', row.listenerSummary || <span className="mus">none</span>],
            ['certificate', row.certSubject || <span className="mus">none</span>],
            ['routes', row.routes],
            ['state', row.stateReason || row.state],
          ]}
        />
        <div style={{ display: 'flex', gap: 6 }}>
          <Button small subtle href={`/trace?hostname=${encodeURIComponent(row.name)}`}>Trace</Button>
          <Button small subtle href={`/sites/${encodeURIComponent(row.name)}`}>Open site</Button>
        </div>
      </div>
      <div className="row" style={{ flex: 1, minWidth: 0, gap: 10, alignItems: 'flex-start', flexWrap: 'wrap' }}>
        {variants.map((v, i) => (
          <div key={v.key || i} style={{ flex: '1 1 200px', minWidth: 160 }}>
            <div className="lbl" style={{ marginBottom: 5 }}>
              Variant {v.key} · {v.nodes} node{v.nodes === 1 ? '' : 's'}{' '}
              {i > 0 && <Badge cls="d">DIFFERS</Badge>}
            </div>
            {v.routes.slice(0, 4).map((rt, j) => (
              <Facet key={j} label={`${rt.pattern} → ${rt.upstream || rt.target || rt.action}`} />
            ))}
            <div className="fct"><span className="m mus">{v.nodeNames.slice(0, 3).join(', ')}{v.nodeNames.length > 3 ? ` … +${v.nodeNames.length - 3}` : ''}</span></div>
          </div>
        ))}
      </div>
    </div>
  )
}

// Ported from internal/web/templates/sites.html against GET /api/sites.
// Layout is design/'s screen 3a: the five stat tiles, one row per hostname,
// and the selected row's variant comparison expanded under it.
// ListSites takes only `q` (hostname/listener substring) and `site` and
// returns every row at once, so that screen's Listener/TLS facets and Sort
// select work on the rows in hand. Omitted: the "Views: All" saved-view menu,
// "Columns · 8", "Cluster: all" (a site row carries no cluster) and the pager.
export function SitesListPage() {
  const [params, setParams] = useSearchParams()
  const q = params.get('q') ?? ''
  const sel = params.get('site') ?? ''
  const [search, setSearch] = useState(q)
  const [listener, setListener] = useState('')
  const [tls, setTls] = useState('')
  const [sort, setSort] = useState('nodes')
  const { data, isPending, isError, error } = useSites({ q, site: sel })

  if (isPending) return <Loading label="Loading sites…" />
  if (isError) return <EmptyPrompt danger title="Could not load sites" body={error.message} />

  const stats = data.stats
  const aliases = data.rows.reduce((n, r) => n + r.aliasCount, 0)
  const listeners = [...new Set(data.rows.map((r) => r.listenerSummary).filter(Boolean))].sort()
  const rows = data.rows
    .filter((r) => !listener || r.listenerSummary === listener)
    .filter((r) => !tls || (/ssl|tls|https/i.test(r.listenerSummary) ? tls === 'on' : tls === 'off'))
    .sort((a, b) => (sort === 'hostname' ? a.name.localeCompare(b.name)
      : sort === 'routes' ? b.routes - a.routes
      : b.nodes - a.nodes || a.name.localeCompare(b.name)))

  const columns: Column<SiteListRow>[] = [
    {
      name: 'Hostname',
      render: (r) => (
        <span className="m">
          <Disclosure open={r.name === sel} />
          <a href={`/sites/${encodeURIComponent(r.name)}`} onClick={(e) => e.stopPropagation()}>{r.name}</a>
          {r.aliasCount > 0 && <span className="mus"> +{r.aliasCount} alias{r.aliasCount === 1 ? '' : 'es'}</span>}
        </span>
      ),
    },
    { name: 'Nodes', width: 80, render: (r) => <span className="m">{r.nodes}</span> },
    { name: 'Listener', width: 110, render: (r) => <span className="m mu">{r.listenerSummary}</span> },
    { name: 'Certificate', width: 150, render: (r) => r.certSubject ? <span className="m mu">{r.certSubject}</span> : <span className="m mus">none</span> },
    { name: 'Routes', width: 70, render: (r) => <span className="m">{r.routes}</span> },
    {
      name: 'Config', width: 120,
      render: (r) => r.variants > 1 ? <Badge cls="d">{r.variants} VARIANTS</Badge> : <Badge cls="v">IDENTICAL</Badge>,
    },
    {
      name: 'State', width: 96,
      render: (r) => <Badge cls={r.state === 'OK' ? 'v' : 'd'} title={r.stateReason}>{r.state}</Badge>,
    },
  ]

  const commit = () => setParams((p) => { if (search) p.set('q', search); else p.delete('q'); return p })
  const select = (name: string) => setParams((p) => { if (name === sel) p.delete('site'); else p.set('site', name); return p })

  return (
    <>
      <PageHeader title="Sites" actions={<Button small href="/api/sites?export=csv">Export CSV</Button>} />

      <QueryBar>
        <span className="fld f">
          <span className="m mus">hostname</span>
          <input value={search} onChange={(e) => setSearch(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && commit()} placeholder="*.corp.example" />
        </span>
        <Button subtle onClick={commit}>Filter</Button>
        <Select value={listener} onChange={setListener}
          options={[{ value: '', text: 'Listener: any' }, ...listeners.map((l) => ({ value: l, text: `Listener: ${l}` }))]} />
        <Select value={tls} onChange={setTls}
          options={[{ value: '', text: 'TLS: any' }, { value: 'on', text: 'TLS: terminated' }, { value: 'off', text: 'TLS: plaintext' }]} />
      </QueryBar>

      <div className="bd">
        <StatRow stats={[
          { label: 'Hostnames', value: stats?.hostnames ?? 0, sub: aliases > 0 ? `incl. ${aliases} aliases` : undefined },
          { label: 'Served on', value: stats?.nodes ?? 0, sub: 'nodes' },
          { label: 'Config variants', value: stats?.variantHosts ?? 0, sub: 'same host, different config', tone: (stats?.variantHosts ?? 0) > 0 ? 'warning' : undefined },
          { label: 'TLS terminated', value: stats?.tlsTerminated ?? 0, sub: `${stats?.plaintext ?? 0} plaintext` },
          { label: 'Certs ≤ 30d', value: stats?.expiringCerts ?? 0, sub: `${stats?.expiringBindings ?? 0} bindings`, tone: (stats?.expiringCerts ?? 0) > 0 ? 'warning' : undefined },
        ]} />

        <Panel z style={{ flex: 1 }}>
          <PanelHeader
            title="Sites"
            meta="one row per hostname · expand a row to compare its config variants"
            actions={
              <Select value={sort} onChange={setSort}
                options={[
                  { value: 'nodes', text: 'Sort: nodes desc' },
                  { value: 'hostname', text: 'Sort: hostname' },
                  { value: 'routes', text: 'Sort: routes desc' },
                ]} />
            }
          />
          <Table
            columns={columns}
            items={rows}
            rowKey={(r) => r.name}
            rowClassName={(r) => (r.name === sel ? 'hl' : undefined)}
            onRowClick={(r) => select(r.name)}
            emptyMessage={q ? 'No sites match this filter.' : 'No sites collected yet.'}
            renderExpanded={(r) => (
              // ListSites hydrates `variants` only for ?site=, so exactly one
              // row can be open at a time.
              r.name === sel && data.variants.length > 0
                ? <VariantDetail row={r} variants={data.variants} />
                : null
            )}
          />
          <PanelFooter>
            <span className="m mus">{sel ? `${sel} selected` : `${rows.length} of ${data.total}`}</span>
          </PanelFooter>
        </Panel>
      </div>
    </>
  )
}
