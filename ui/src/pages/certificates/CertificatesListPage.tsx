import { useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useCertificate, useCertificates } from '../../api/queries/certificates'
import type { CertificateListItem } from '../../api/pb/nagipath/api/v1/certificates_pb'
import { PageHeader } from '../../components/shared/PageHeader'
import { PanelHeader } from '../../components/shared/PanelHeader'
import { StatRow } from '../../components/shared/StatTile'
import {
  Badge, Button, Disclosure, EmptyPrompt, Facet, Kv, Loading, Panel,
  PanelFooter, QueryBar, Select, Table, type Column,
} from '../../components/ui'

function daysLeft(notAfter: string): number {
  return Math.floor((new Date(notAfter).getTime() - Date.now()) / 86400e3)
}

function expiryBadge(notAfter: string) {
  const d = daysLeft(notAfter)
  return <Badge cls={d < 0 ? 'r' : d <= 30 ? 'd' : 'v'}>{d < 0 ? 'EXPIRED' : `${d} DAYS`}</Badge>
}

function keyLabel(c: CertificateListItem): string {
  if (!c.keyAlgorithm) return '—'
  return c.keyBits > 0 ? `${c.keyAlgorithm.toLowerCase()}${c.keyBits}` : c.keyAlgorithm.toLowerCase()
}

// design/'s "Weak key · rsa2048 < 2027 policy" tile. RSA below 3072 bits is the
// line every public CA and NIST SP 800-57 draw for 2030+; there is no
// configurable policy behind it yet, so the threshold is stated here.
function weakKey(c: CertificateListItem): boolean {
  return /rsa/i.test(c.keyAlgorithm) && c.keyBits > 0 && c.keyBits < 3072
}

// design/'s 2f detail, expanded under the fingerprint's own row: its metadata
// and where it is served, grouped by cluster. Serial/SAN/sig only exist on
// GET /certificates/{id} (the list query doesn't select them), so opening a row
// fetches that one certificate.
function CertDetail({ id }: { id: number }) {
  const { data, isPending } = useCertificate(id, 'overview')
  if (isPending || !data?.cert) return <Loading label="Loading certificate…" />

  const c = data.cert
  const byCluster = new Map<string, typeof data.bindings>()
  for (const b of data.bindings) {
    const k = b.clusterName || 'unassigned'
    byCluster.set(k, [...(byCluster.get(k) ?? []), b])
  }

  return (
    <div className="row" style={{ alignItems: 'flex-start' }}>
      <div className="col" style={{ flex: '0 0 340px', gap: 6 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <span className="lbl">{c.subjectCn || '(no CN)'}</span>
          {expiryBadge(c.notAfter)}
        </div>
        <Kv labelWidth={82} rows={[
          ['SHA-256', c.fingerprint],
          ['serial', c.serial || <span className="mus">—</span>],
          ['SAN', c.sans.length > 0 ? c.sans.join(', ') : <span className="mus">none</span>],
          ['valid', `${c.notBefore?.slice(0, 10) || '?'} → ${c.notAfter?.slice(0, 10) || '?'}`],
          ['key', c.keyBits > 0 ? `${c.keyAlgorithm.toLowerCase()}${c.keyBits}` : c.keyAlgorithm || '—'],
          ['sig', c.sigAlgorithm || <span className="mus">—</span>],
        ]} />
        <div><Button small subtle href={`/certificates/${id}`}>Open certificate</Button></div>
      </div>

      <div className="col" style={{ flex: 1, gap: 2 }}>
        <span className="lbl">bindings · {data.bindingCount} · grouped by cluster</span>
        {[...byCluster].map(([cluster, bs]) => (
          <div key={cluster}>
            <div className="m mus" style={{ padding: '4px 0 2px' }}>{cluster} · {bs.length}</div>
            {bs.map((b, i) => (
              <Facet key={i} label={b.node} count={`${b.siteNames || b.instance}${b.port > 0 ? `:${b.port}` : ''}`} />
            ))}
          </div>
        ))}
        {data.bindings.length === 0 && <div className="m mus">Not bound anywhere in the current snapshots.</div>}
      </div>
    </div>
  )
}

// Ported from internal/web/templates/certificates.html against GET
// /api/certificates (internal/api/certificateservice.go), laid out as design/'s
// screen 2f: five expiry/key tiles, one row per fingerprint, and the selected
// fingerprint's metadata + bindings expanded under it.
// Omitted from that screen: the "Views: Expiring" saved-view menu, the
// per-page select and the pager (ListCertificates returns every certificate at
// once), and the query-language
// filter box — the real server-side filter is the expiry bucket, so that is
// the control shown rather than a text field that would have to parse
// "expires_in < 30d".
export function CertificatesListPage() {
  const [params, setParams] = useSearchParams()
  const expires = params.get('expires') ?? ''
  const issuer = params.get('issuer') ?? ''
  const cluster = params.get('cluster') ?? ''
  const key = params.get('key') ?? ''
  const includeCAs = params.get('include_cas') === '1'
  const selected = params.get('cert') ? Number(params.get('cert')) : 0
  const [filter, setFilter] = useState('')
  const [sort, setSort] = useState('expiry')
  const { data, isPending, isError, error } = useCertificates({ expires, issuer, cluster, includeCAs })

  if (isPending) return <Loading label="Loading certificates…" />
  if (isError) return <EmptyPrompt danger title="Could not load certificates" body={error.message} />

  const keys = [...new Set(data.list.map(keyLabel).filter((k) => k !== '—'))].sort()
  const rows = data.list.filter((c) => {
    if (key && keyLabel(c) !== key) return false
    if (!filter) return true
    const hay = `${c.subjectCn} ${c.sans.join(' ')} ${c.issuerDn}`.toLowerCase()
    return hay.includes(filter.toLowerCase())
  }).sort((a, b) => (sort === 'subject' ? (a.subjectCn || '').localeCompare(b.subjectCn || '')
    : sort === 'bindings' ? b.bindings - a.bindings
    : daysLeft(a.notAfter) - daysLeft(b.notAfter)))

  const bucket = (pred: (d: number) => boolean) => {
    const hit = data.list.filter((c) => pred(daysLeft(c.notAfter)))
    return { certs: hit.length, bindings: hit.reduce((n, c) => n + c.bindings, 0) }
  }
  const expired = bucket((d) => d < 0)
  const in7 = bucket((d) => d >= 0 && d <= 7)
  const in30 = bucket((d) => d >= 0 && d <= 30)
  const weak = data.list.filter(weakKey).length

  const columns: Column<CertificateListItem>[] = [
    {
      name: 'Subject · SAN',
      render: (c) => (
        <span className="m">
          <Disclosure open={Number(c.id) === selected} />
          <a href={`/certificates/${c.id}`} onClick={(e) => e.stopPropagation()} style={{ fontWeight: 500 }}>{c.subjectCn || '(no CN)'}</a>
          {c.sans.length > 1 && <span className="mus"> +{c.sans.length - 1} SAN</span>}
        </span>
      ),
    },
    { name: 'Issuer', width: 150, render: (c) => <span className="m mu">{c.issuerDn || (c.isCa ? 'self-signed CA' : '—')}</span> },
    { name: 'Expires', width: 150, render: (c) => <span className="m mu">{c.notAfter?.slice(0, 10) || '—'}</span> },
    { name: 'Bindings', width: 90, render: (c) => <span className="m">{c.bindings}</span> },
    { name: 'Key', width: 86, render: (c) => <span className={weakKey(c) ? 'm' : 'm mu'} style={weakKey(c) ? { color: '#8a5300' } : undefined}>{keyLabel(c)}</span> },
    { name: 'State', width: 92, render: (c) => expiryBadge(c.notAfter) },
  ]

  const set = (k: string, v: string) => setParams((p) => { if (v) p.set(k, v); else p.delete(k); return p })

  return (
    <>
      <PageHeader title="Certificates" actions={<Button small href="/api/certificates?export=csv">Export CSV</Button>} />

      <QueryBar>
        <span className="fld f">
          <span className="m mus">filter</span>
          <input value={filter} onChange={(e) => setFilter(e.target.value)} placeholder="subject, SAN or issuer" />
        </span>
        <Select
          options={[
            { value: '', text: 'Expires: any time' },
            { value: 'expired', text: 'Expires: expired' },
            { value: '7d', text: 'Expires: ≤ 7 days' },
            { value: '30d', text: 'Expires: ≤ 30 days' },
            { value: '90d', text: 'Expires: ≤ 90 days' },
          ]}
          value={expires}
          onChange={(v) => set('expires', v)}
        />
        <Select
          options={[{ value: '', text: 'Issuer: all' }, ...data.issuers.map((i) => ({ value: i, text: i }))]}
          value={issuer}
          onChange={(v) => set('issuer', v)}
        />
        <Select
          options={[{ value: '', text: 'Key: all' }, ...keys.map((k) => ({ value: k, text: `Key: ${k}` }))]}
          value={key}
          onChange={(v) => set('key', v)}
        />
        <Select
          options={[{ value: '', text: 'Cluster: all' }, ...data.clusters.map((c) => ({ value: c.id.toString(), text: `Cluster: ${c.name}` }))]}
          value={cluster}
          onChange={(v) => set('cluster', v)}
        />
        <Button subtle onClick={() => set('include_cas', includeCAs ? '' : '1')}>
          {includeCAs ? 'Hide CA certificates' : 'Show CA certificates'}
        </Button>
      </QueryBar>

      <div className="bd">
          <StatRow stats={[
            { label: 'Expired', value: expired.certs, sub: `${expired.bindings} bindings`, tone: expired.certs > 0 ? 'danger' : undefined },
            { label: '≤ 7 days', value: in7.certs, sub: `${in7.bindings} bindings`, tone: in7.certs > 0 ? 'warning' : undefined },
            { label: '≤ 30 days', value: in30.certs, sub: `${in30.bindings} bindings` },
            { label: 'Total distinct', value: data.list.length, sub: 'by fingerprint' },
            { label: 'Weak key', value: weak, sub: 'rsa below 3072', tone: weak > 0 ? 'warning' : undefined },
          ]} />

          <Panel z style={{ flex: 1 }}>
            <PanelHeader
              title="Certificates"
              meta="one row per fingerprint · expand a row for its SANs and bindings"
              actions={
                <Select value={sort} onChange={setSort}
                  options={[
                    { value: 'expiry', text: 'Sort: expiry' },
                    { value: 'subject', text: 'Sort: subject' },
                    { value: 'bindings', text: 'Sort: bindings desc' },
                  ]} />
              }
            />
            <Table<CertificateListItem>
              items={rows}
              columns={columns}
              rowKey={(c) => c.id.toString()}
              rowClassName={(c) => (Number(c.id) === selected ? 'hl' : undefined)}
              onRowClick={(c) => set('cert', Number(c.id) === selected ? '' : c.id.toString())}
              emptyMessage="No certificates match this filter."
              renderExpanded={(c) => (Number(c.id) === selected ? <CertDetail id={selected} /> : null)}
            />
            <PanelFooter>
              <span className="m mus">{rows.length} of {data.list.length} in filter</span>
            </PanelFooter>
          </Panel>
      </div>
    </>
  )
}
