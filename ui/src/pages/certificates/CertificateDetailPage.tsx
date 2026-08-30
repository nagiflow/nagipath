import { useState } from 'react'
import { useParams, useSearchParams } from 'react-router-dom'
import { useCertificate } from '../../api/queries/certificates'
import type { CertBinding, CertFilePath } from '../../api/pb/nagipath/api/v1/certificates_pb'
import { PageHeader } from '../../components/shared/PageHeader'
import { PanelHeader } from '../../components/shared/PanelHeader'
import { StatRow } from '../../components/shared/StatTile'
import {
  Badge, Button, EmptyPrompt, Field, Kv, Loading, Panel,
  PanelFooter, Select, Table, Tabs,
} from '../../components/ui'
import { daysUntil } from '../../lib/time'

// Ported from internal/web/templates/certificate.html against
// GET /api/certificates/{id} (internal/api/certificateservice.go), laid out as
// design/'s screen 7b: the Certificate + Spread column, five tiles, every
// binding, and the distinct file paths.
// Omitted from that screen: the Chain panel and the "Chain"/"History" tabs
// (nagipath stores one certificate per fingerprint with no issuer linkage and
// keeps no per-fingerprint timeline, so a chain or history view would be
// invented), "Export bindings" and "Find replacements" (no endpoint behind
// either), the file rows' mode/owner/modified columns and the bindings'
// "State" column — collection records the cert's file path, not its stat
// output, and never opens a TLS connection, so file ownership and per-binding
// chain completeness are both unknown.
export function CertificateDetailPage() {
  const { id = '' } = useParams()
  const [params, setParams] = useSearchParams()
  const raw = params.get('tab') ?? ''
  const tab = raw === 'bindings' || raw === 'files' ? raw : 'overview'
  const [filter, setFilter] = useState('')
  const [cluster, setCluster] = useState('')
  const { data, isPending, isError, error } = useCertificate(Number(id), tab)

  if (isPending) return <Loading label="Loading certificate…" />
  if (isError) return <EmptyPrompt danger title="Could not load this certificate" body={error.message} />

  const c = data.cert
  if (!c) return <EmptyPrompt title="No such certificate" body="It may have been dropped by retention." />

  const days = daysUntil(c.notAfter)
  const key = c.keyBits > 0 ? `${c.keyAlgorithm.toLowerCase()}${c.keyBits}` : c.keyAlgorithm || '—'
  const weak = /rsa/i.test(c.keyAlgorithm) && c.keyBits > 0 && c.keyBits < 3072
  const sites = new Set(data.bindings.flatMap((b) => b.siteNames.split(',').filter(Boolean))).size
  const nodes = new Set(data.bindings.map((b) => b.node)).size
  const clusters = new Set(data.bindings.map((b) => b.clusterName).filter(Boolean)).size
  const bindingClusters = [...new Set(data.bindings.map((b) => b.clusterName).filter(Boolean))].sort()
  const bindings = data.bindings
    .filter((b) => !filter || `${b.node} ${b.siteNames} ${b.filePath}`.toLowerCase().includes(filter.toLowerCase()))
    .filter((b) => !cluster || b.clusterName === cluster)

  return (
    <>
      <PageHeader
        title={c.subjectCn || '(no CN)'}
        badge={<Badge cls={days < 0 ? 'r' : days <= 30 ? 'd' : 'v'}>{days < 0 ? `EXPIRED ${-days} DAYS AGO` : `EXPIRES IN ${days} DAYS`}</Badge>}
        meta={[c.issuerDn || (c.selfSigned ? 'self-signed' : null), key, `${data.bindingCount} binding${data.bindingCount === 1 ? '' : 's'}`].filter(Boolean).join(' · ')}
        actions={c.sans.length > 0 && <Button small href={`/trace?hostname=${encodeURIComponent(c.sans[0])}`}>Trace entry points</Button>}
      />
      <Tabs
        selected={tab}
        onSelect={(t) => setParams((p) => { if (t === 'overview') p.delete('tab'); else p.set('tab', t); return p })}
        tabs={[
          { id: 'overview', label: 'Overview' },
          { id: 'bindings', label: `Bindings · ${data.bindingCount}` },
          { id: 'files', label: `Files · ${data.fileCount}` },
        ]}
      />

      <div className="bd" style={{ flexDirection: 'row' }}>
        <div className="col" style={{ flex: '0 0 320px' }}>
          <Panel style={{ padding: '10px 12px' }}>
            <div className="lbl" style={{ marginBottom: 6 }}>Certificate</div>
            <Kv labelWidth={82} rows={[
              ['SHA-256', c.fingerprint],
              ['serial', c.serial || <span className="mus">—</span>],
              ['subject', c.subjectDn || c.subjectCn || <span className="mus">—</span>],
              ['SAN', c.sans.length > 0 ? c.sans.join(', ') : <span className="mus">none</span>],
              ['valid', `${c.notBefore?.slice(0, 10) || '?'} → ${c.notAfter?.slice(0, 10) || '?'}`],
              ['key', key],
              ['sig', c.sigAlgorithm || <span className="mus">—</span>],
              ['kind', c.isCa ? 'CA certificate' : c.selfSigned ? 'self-signed leaf' : 'leaf'],
            ]} />
          </Panel>

          <Panel style={{ padding: '10px 12px', flex: 1, minHeight: 0 }}>
            <div className="lbl" style={{ marginBottom: 6 }}>Spread</div>
            <Kv labelWidth={82} rows={[
              ['nodes', nodes],
              ['sites', sites],
              ['clusters', clusters || <span className="mus">none assigned</span>],
              ['file paths', data.fileCount],
              ['first seen', c.firstSeen ? c.firstSeen.slice(0, 10) : <span className="mus">—</span>],
              ['last seen', c.lastSeen ? c.lastSeen.slice(0, 10) : <span className="mus">—</span>],
            ]} />
          </Panel>
        </div>

        <div className="col" style={{ flex: 1, minWidth: 0 }}>
          {tab === 'overview' && <StatRow stats={[
            {
              label: 'Expires', value: days < 0 ? `${-days}d ago` : `${days}d`,
              sub: c.notAfter ? new Date(c.notAfter).toLocaleString() : undefined,
              tone: days < 0 ? 'danger' : days <= 30 ? 'warning' : undefined,
            },
            { label: 'Bindings', value: data.bindingCount, sub: `${nodes} node${nodes === 1 ? '' : 's'}` },
            { label: 'Sites', value: sites, sub: `${clusters} cluster${clusters === 1 ? '' : 's'}` },
            { label: 'Files on disk', value: data.fileCount, sub: data.fileCount > 1 ? `${data.fileCount} paths differ` : 'one path' },
            { label: 'Key', value: key, sub: weak ? 'below 3072-bit policy' : 'meets 3072-bit policy', tone: weak ? 'warning' : undefined },
          ]} />}

          {tab !== 'files' && (
          <Panel z style={{ flex: 1.3 }}>
            <PanelHeader
              title="Bindings"
              meta="node · site · listener"
              actions={
                <>
                  <Field placeholder="filter" value={filter} onChange={setFilter} />
                  <Select value={cluster} onChange={setCluster}
                    options={[{ value: '', text: 'Cluster: all' }, ...bindingClusters.map((n) => ({ value: n, text: `Cluster: ${n}` }))]} />
                </>
              }
            />
            <Table<CertBinding>
              items={bindings}
              rowKey={(b) => `${b.instanceId}-${b.fileId}-${b.port}-${b.siteNames}`}
              columns={[
                { name: 'Node', width: 200, render: (b) => <a className="m" href={`/nodes/${b.nodeId}?process=${b.instanceId}`}>{b.node}</a> },
                { name: 'Site', width: 180, render: (b) => b.siteNames ? <span className="m mu">{b.siteNames}</span> : <span className="m mus">{b.instance}</span> },
                { name: 'Listener', width: 96, render: (b) => <span className="m mu">{b.port > 0 ? `${b.port} ssl` : '—'}</span> },
                { name: 'File path', render: (b) => <span className="m mu">{b.filePath || '—'}</span> },
                { name: 'Cluster', width: 110, render: (b) => <span className="m mu">{b.clusterName || '—'}</span> },
                { name: 'Bundle', width: 110, render: (b) => <span className="m mu">{b.combinedPem ? 'leaf + key' : 'leaf'}</span> },
              ]}
              emptyMessage={filter ? 'No binding matches this filter.' : 'Not bound anywhere in the current snapshots.'}
            />
            <PanelFooter>
              <span className="m mus">{bindings.length} of {data.bindingCount}</span>
            </PanelFooter>
          </Panel>
          )}

          {tab !== 'bindings' && (
          <Panel z style={tab === 'files' ? { flex: 1 } : { flex: '0 0 158px' }}>
            <PanelHeader title="Files on disk" meta="path only · contents never read" />
            <Table<CertFilePath>
              items={data.filePaths}
              rowKey={(f) => f.path}
              columns={[
                { name: 'Path', render: (f) => <span className="m">{f.path}</span> },
                { name: 'Nodes', width: 90, render: (f) => <span className="m">{f.nodeCount}</span> },
                { name: 'Bundle', width: 130, render: (f) => <span className="m mu">{f.bundleType}</span> },
              ]}
              emptyMessage="No file path recorded for this certificate."
            />
          </Panel>
          )}
        </div>
      </div>
    </>
  )
}
