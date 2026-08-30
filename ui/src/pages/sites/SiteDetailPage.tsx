import { useState } from 'react'
import { useParams, useSearchParams } from 'react-router-dom'
import { useSite } from '../../api/queries/sites'
import type { SiteCertBinding, SiteDetailRoute, SiteNodeRow, SiteUpstreamMember } from '../../api/pb/nagipath/api/v1/sites_pb'
import { actionPhrase, matchPhrase } from '../../lib/routeLanguage'
import { PageHeader } from '../../components/shared/PageHeader'
import { PanelHeader } from '../../components/shared/PanelHeader'
import { StatRow } from '../../components/shared/StatTile'
import {
  Badge, Button, EmptyPrompt, Facet, Field, Kv, Loading, Panel,
  PanelFooter, Select, Table, Tabs,
} from '../../components/ui'

// design/'s screen 7a's node table — same columns on the Overview panel and
// the Nodes tab, so it's defined once.
const nodeColumns = [
  { name: 'Node', width: 186, render: (r: SiteNodeRow) => <a className="m" href={`/nodes/${r.nodeId}`}>{r.nodeName}</a> },
  { name: 'Cluster', width: 96, render: (r: SiteNodeRow) => <span className="m mu">{r.cluster || '—'}</span> },
  { name: 'Variant', width: 70, render: (r: SiteNodeRow) => <span className="m mu">{r.variant}</span> },
  { name: 'Listener', width: 104, render: (r: SiteNodeRow) => <span className="m mu">{r.listener}</span> },
  { name: 'Certificate', width: 140, render: (r: SiteNodeRow) => r.certificate ? <span className="m mu">{r.certificate}</span> : <span className="m mus">none</span> },
  { name: 'Last collected', width: 110, render: (r: SiteNodeRow) => <span className="m mu">{r.lastColl ? new Date(r.lastColl).toLocaleTimeString() : '—'}</span> },
  { name: 'State', width: 112, render: (r: SiteNodeRow) => <Badge cls={r.state === 'OK' ? 'v' : r.state.includes('EXPIRED') ? 'r' : 'd'} title={r.stateReason}>{r.state}</Badge> },
]

// design/'s 7a shows the same route table on Overview and on its Routes tab.
const routeColumns = [
  { name: '#', width: 34, render: (r: SiteDetailRoute) => <span className="m mus">{r.ordinal}</span> },
  { name: 'Match', width: 220, render: (r: SiteDetailRoute) => <span className="m">{matchPhrase(r.matchType, r.pattern)}</span> },
  { name: 'Action', width: 150, render: (r: SiteDetailRoute) => <span className="m mu">{actionPhrase(r.action, r.target)}</span> },
  { name: 'Sends it to', render: (r: SiteDetailRoute) => r.action ? <span className="m">{r.action}</span> : <span className="m mu">{r.target || '—'}</span> },
  { name: 'Variant', width: 78, render: (r: SiteDetailRoute) => <span className="m mus">{r.variant}</span> },
]

// Ported from internal/web/templates/site.html against GET /api/sites/{name}
// (internal/api/sites.go), laid out as design/'s screen 7a: a 300px column of
// Serving / Certificate / Upstreams-reached panels beside the stat row, the
// route projection for the selected variant and the nodes serving the site.
// Omitted from that screen: the "History" tab (no history query behind it), the
// "Also does" route column (route.also_does is never populated by any parser
// yet), and the "first seen"/"default server" kv rows and the "N regex" /
// "A 22 · B 2" stat sub-labels, which SiteOverview doesn't carry.
export function SiteDetailPage() {
  const { name = '' } = useParams()
  const [params, setParams] = useSearchParams()
  const tab = params.get('tab') ?? 'overview'
  const variant = params.get('variant') ?? undefined
  const [pathFilter, setPathFilter] = useState('')

  const { data, isPending, isError, error } = useSite(name, { tab, variant })

  if (isPending) return <Loading label="Loading site…" />
  if (isError) {
    const notFound = error.message?.toLowerCase().includes('no node')
    return <EmptyPrompt danger={!notFound} title={notFound ? 'No such site' : 'Could not load this site'} body={error.message} />
  }

  const o = data.overview
  const stats = o?.stats
  const members = o?.upstreams.reduce((n, u) => n + u.members, 0) ?? 0
  const certDays = stats?.certDays ?? 0
  const routes = (o?.routes ?? []).filter((r) => !pathFilter || r.pattern.toLowerCase().includes(pathFilter.toLowerCase()))

  const setTab = (id: string) => setParams((p) => { if (id === 'overview') p.delete('tab'); else p.set('tab', id); return p })

  return (
    <>
      <PageHeader
        title={data.name}
        badge={o && o.variants > 1 ? <Badge cls="d">{o.variants} VARIANTS</Badge> : undefined}
        meta={o ? [
          `${o.nodes} node${o.nodes === 1 ? '' : 's'}`,
          o.clusters.length > 0 ? `${o.clusters.length} cluster${o.clusters.length === 1 ? '' : 's'}` : null,
          o.aliases.length > 0 ? `aliases ${o.aliases.join(', ')}` : null,
        ].filter(Boolean).join(' · ') : undefined}
        actions={
          <>
            {data.variantOpts.length > 1 && (
              <Select
                options={data.variantOpts.map((v) => ({ value: v.key, text: v.label }))}
                value={data.variantOpts.find((v) => v.on)?.key ?? data.variantOpts[0].key}
                onChange={(v) => setParams((p) => { p.set('variant', v); return p })}
              />
            )}
            <Button small href={`/trace?hostname=${encodeURIComponent(data.name)}`}>Trace</Button>
          </>
        }
      />

      <Tabs
        selected={tab}
        onSelect={setTab}
        tabs={data.tabs.map((t) => ({
          id: t.href.includes('tab=') ? new URLSearchParams(t.href.split('?')[1]).get('tab')! : 'overview',
          label: t.count > 0 ? `${t.label} · ${t.count}` : t.label,
        }))}
      />

      <div className="bd" style={tab === 'overview' ? { flexDirection: 'row' } : undefined}>
        {tab === 'overview' && o && (
          <>
            <div className="col" style={{ flex: '0 0 300px' }}>
              <Panel style={{ padding: '10px 12px' }}>
                <div className="lbl" style={{ marginBottom: 6 }}>Serving</div>
                <Kv rows={[
                  ['listener', o.listenerSummary || '—'],
                  ['flags', o.listenerFlags || '—'],
                  ['aliases', o.aliases.length > 0 ? o.aliases.join(', ') : <span className="mus">none</span>],
                  ['clusters', o.clusters.length > 0 ? o.clusters.map((c) => `${c.name} ${c.nodes}`).join(', ') : <span className="mus">none</span>],
                ]} />
              </Panel>

              <Panel style={{ padding: '10px 12px' }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 6 }}>
                  <span className="lbl">Certificate</span>
                  {o.certSubject && <Badge cls={certDays <= 0 ? 'r' : certDays <= 30 ? 'd' : 'v'}>{certDays <= 0 ? 'EXPIRED' : `${certDays} DAYS`}</Badge>}
                </div>
                {o.certSubject ? (
                  <>
                    <Kv rows={[
                      ['subject', o.certSubject],
                      ['issuer', o.certIssuer || '—'],
                      ['expires', o.certExpiry ? new Date(o.certExpiry).toLocaleDateString() : '—'],
                      ['bindings', `${o.certBindings} on this site`],
                    ]} />
                    {o.certUncovered.length > 0 && (
                      <div className="m mus" style={{ marginTop: 7 }}>
                        not covered by this certificate: {o.certUncovered.join(', ')}
                      </div>
                    )}
                  </>
                ) : (
                  <div className="m mus">Served plaintext — no certificate bound.</div>
                )}
              </Panel>

              <Panel style={{ padding: '10px 12px', flex: 1, minHeight: 0 }}>
                <div className="lbl" style={{ marginBottom: 6 }}>Upstreams reached</div>
                {o.upstreams.length === 0
                  ? <div className="m mus">None — every route answers locally.</div>
                  : o.upstreams.map((u) => (
                    <Facet
                      key={`${u.name}-${u.variant}`}
                      label={u.name}
                      count={`${u.members} member${u.members === 1 ? '' : 's'}${u.variant ? ` · variant ${u.variant}` : ''}`}
                    />
                  ))}
              </Panel>
            </div>

            <div className="col" style={{ flex: 1 }}>
              <StatRow stats={[
                { label: 'Nodes', value: stats?.nodes ?? 0, sub: `${o.clusters.length} cluster${o.clusters.length === 1 ? '' : 's'}` },
                { label: 'Routes', value: stats?.routes ?? 0 },
                { label: 'Variants', value: stats?.variants ?? 0, tone: (stats?.variants ?? 0) > 1 ? 'warning' : undefined },
                { label: 'Upstreams', value: stats?.upstreams ?? 0, sub: `${members} member${members === 1 ? '' : 's'}` },
                { label: 'Cert', value: o.certSubject ? `${certDays}d` : '—', sub: stats?.certSubject || undefined, tone: o.certSubject && certDays <= 30 ? 'warning' : undefined },
              ]} />

              <Panel z style={{ flex: 1.15 }}>
                <PanelHeader
                  title="Routes"
                  meta={`variant ${o.variantKey || 'A'} · match order`}
                  actions={<Field placeholder="filter path" value={pathFilter} onChange={setPathFilter} />}
                />
                <Table<SiteDetailRoute>
                  items={routes}
                  rowKey={(r) => r.ordinal.toString()}
                  columns={routeColumns}
                  emptyMessage={pathFilter ? 'No route matches this filter.' : 'No routes on this variant.'}
                />
                <PanelFooter>
                  <span className="m mus">{routes.length} of {stats?.routes ?? routes.length}</span>
                </PanelFooter>
              </Panel>

              <Panel z style={{ flex: 1 }}>
                <PanelHeader title="Nodes serving this site" />
                <Table<SiteNodeRow> items={data.nodes} rowKey={(r) => r.nodeId.toString()} columns={nodeColumns} emptyMessage="No nodes." />
                <PanelFooter>
                  <span className="m mus">{data.nodes.length} of {o.nodes}</span>
                </PanelFooter>
              </Panel>
            </div>
          </>
        )}

        {tab === 'routes' && o && (
          <Panel z style={{ flex: 1 }}>
            <PanelHeader
              title="Routes"
              meta={`variant ${o.variantKey || 'A'} · match order`}
              actions={<Field placeholder="filter path" value={pathFilter} onChange={setPathFilter} />}
            />
            <Table<SiteDetailRoute>
              items={routes}
              rowKey={(r) => r.ordinal.toString()}
              columns={routeColumns}
              emptyMessage={pathFilter ? 'No route matches this filter.' : 'No routes on this variant.'}
            />
            <PanelFooter>
              <span className="m mus">{routes.length} of {stats?.routes ?? routes.length}</span>
            </PanelFooter>
          </Panel>
        )}

        {tab === 'nodes' && (
          <Panel z style={{ flex: 1 }}>
            <PanelHeader title="Nodes serving this site" meta="all variants" />
            <Table<SiteNodeRow> items={data.nodes} rowKey={(r) => r.nodeId.toString()} columns={nodeColumns} emptyMessage="No nodes." />
          </Panel>
        )}

        {tab === 'upstreams' && (
          <Panel z style={{ flex: 1 }}>
            <PanelHeader title="Upstream members" meta="one row per member, per node" />
            <Table<SiteUpstreamMember>
              items={data.upstreams}
              rowKey={(r) => `${r.upstream}-${r.host}-${r.port}-${r.nodeName}`}
              columns={[
                { name: 'Upstream', width: 180, render: (r) => <span className="m">{r.upstream}</span> },
                { name: 'Member', render: (r) => <span className="m">{r.scheme ? `${r.scheme}://` : ''}{r.host}:{r.port}</span> },
                { name: 'Weight', width: 70, render: (r) => <span className="m mu">{r.weight || '—'}</span> },
                { name: 'Flags', width: 140, render: (r) => <span className="m mu">{r.flags || '—'}</span> },
                { name: 'Node', width: 186, render: (r) => <span className="m mu">{r.nodeName}</span> },
              ]}
              emptyMessage="No upstreams."
            />
          </Panel>
        )}

        {tab === 'certificates' && (
          <Panel z style={{ flex: 1 }}>
            <PanelHeader title="Certificates bound" meta="across every node serving this hostname" />
            <Table<SiteCertBinding>
              items={data.certs}
              rowKey={(r) => `${r.subject}-${r.notAfter}`}
              columns={[
                { name: 'Subject', render: (r) => <span className="m">{r.subject || '(no CN)'}</span> },
                { name: 'Issuer', width: 180, render: (r) => <span className="m mu">{r.issuer || '—'}</span> },
                { name: 'Expires', width: 110, render: (r) => <span className="m mu">{r.notAfter ? new Date(r.notAfter).toLocaleDateString() : '—'}</span> },
                { name: 'In', width: 90, render: (r) => <Badge cls={r.expiryDays <= 0 ? 'r' : r.expiryDays <= 30 ? 'd' : 'v'}>{r.expiryDays <= 0 ? 'EXPIRED' : `${r.expiryDays} DAYS`}</Badge> },
                { name: 'Bindings', width: 80, render: (r) => <span className="m">{r.bindings}</span> },
                { name: 'Not covered', render: (r) => r.uncovered.length > 0 ? <span className="m mu">{r.uncovered.join(', ')}</span> : <span className="m mus">—</span> },
              ]}
              emptyMessage="No certificates bound to this site."
            />
          </Panel>
        )}
      </div>
    </>
  )
}
