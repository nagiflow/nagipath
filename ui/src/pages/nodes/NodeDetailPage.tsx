import { useState } from 'react'
import { useNavigate, useParams, useSearchParams, type SetURLSearchParams } from 'react-router-dom'
import { APIError } from '../../api/client'
import { useChangeNodeCredential, useCollectNode, useDecideHostKey, useDeleteNode, useNode } from '../../api/queries/nodes'
import { useSession } from '../../api/queries/session'
import type { Credential, DriftFinding, FileRef, NodeCertBinding, NodeDetailResponse, Site, UpstreamMember, UpstreamPool } from '../../api/pb/nagipath/api/v1/nodes_pb'
import { PageHeader } from '../../components/shared/PageHeader'
import { PanelHeader } from '../../components/shared/PanelHeader'
import { StatRow } from '../../components/shared/StatTile'
import {
  Badge,
  Button,
  CallOut,
  CodeBlock,
  ConfirmModal,
  Disclosure,
  EmptyPrompt,
  Facet,
  Kv,
  Loading,
  Modal,
  Panel,
  FlexSpacer,
  PanelFooter,
  QueryBar,
  Select,
  Table,
  Tabs,
} from '../../components/ui'
import { daysUntil } from '../../lib/time'

// design/'s screens 3c/2o/2p all print "in 6 days" style expiry as a coloured
// pill; one rule for the three of them.
function expiryBadge(notAfter: string) {
  if (!notAfter) return <span className="m mus">—</span>
  const days = Math.floor((new Date(notAfter).getTime() - Date.now()) / 86400e3)
  return <Badge cls={days <= 0 ? 'r' : days <= 30 ? 'd' : 'v'}>{days <= 0 ? 'EXPIRED' : `${days} DAYS`}</Badge>
}

function fmtTime(iso: string): string {
  return iso ? `${new Date(iso).toISOString().slice(11, 16)}Z` : 'never'
}

// Ported from internal/web/templates/node.html against
// GET /api/nodes/{id}[/{tab}] (internal/api/nodes.go) — same tabs, same
// process picker, same running-collection self-refresh (now a TanStack Query
// refetchInterval instead of htmx's `hx-trigger="every 3s"`, see
// api/queries/nodes.ts's useNode). Host-key approve/reject moved to
// POST /api/hostkeys/{id}/decide. Rendered against design/'s screens 3c
// (Node detail), 5a (Node › Routes) and 5b (Node › Config files).
export function NodeDetailPage() {
  const { id = '' } = useParams()
  const nodeID = Number(id)
  const [params, setParams] = useSearchParams()
  const navigate = useNavigate()
  const { data: session } = useSession()
  // A ?tab= this page has no panel for would otherwise render an empty body.
  const raw = params.get('tab') ?? ''
  const tab = ['sites', 'routes', 'upstreams', 'certificates', 'files', 'drift'].includes(raw) ? raw : ''
  const process = params.get('process') ? Number(params.get('process')) : undefined
  const pool = params.get('pool') ? Number(params.get('pool')) : undefined
  const file = params.get('file') ? Number(params.get('file')) : undefined
  const site = params.get('site') ?? undefined
  const route = params.get('route') ?? undefined

  const { data, isPending, isError, error } = useNode(nodeID, { tab, process, pool, file, site, route })
  const collect = useCollectNode(nodeID)
  const del = useDeleteNode()
  const decide = useDecideHostKey()
  const [confirmDelete, setConfirmDelete] = useState(false)
  // design/'s 2o/2p/5a/5b put each tab's filters in a query bar above the
  // body rather than in the panel header, so the state lives here with the
  // tab that owns it.
  const [tabQ, setTabQ] = useState('')
  const [resolution, setResolution] = useState('')
  const [poolSort, setPoolSort] = useState('members')
  const [expiry, setExpiry] = useState('')
  const [testPath, setTestPath] = useState('')

  if (isPending) return <Loading label="Loading node…" />
  if (isError) {
    const notFound = error instanceof APIError && error.status === 404
    return (
      <EmptyPrompt
        danger={!notFound}
        title={notFound ? 'No such node' : 'Could not load this node'}
        body={error.message}
      />
    )
  }

  const isAdmin = session?.user?.role === 'admin'
  const setProcess = (pid: bigint) => setParams((p) => { p.set('process', pid.toString()); return p })

  // Overview's own href is just `/nodes/41[?process=…]` — no extra path
  // segment to read a tab name off, unlike every other tab's
  // `/nodes/41/sites[?process=…]`. Splitting on '/' and taking the last
  // piece picked up the node id instead ('41', never falsy), so Overview's
  // computed id never matched `selected` and its underline never showed.
  const tabs = data.tabs.map((t) => {
    const segments = t.href.split('?')[0].split('/').filter(Boolean) // ['nodes', '41', 'sites'?]
    return {
      id: segments[2] ?? 'overview',
      label: t.count > 0 ? `${t.label} · ${t.count}` : t.label,
    }
  })

  const failures = data.node?.consecutiveFailures ?? 0
  const state = !data.node?.lastCollection ? 'pending' : failures === 0 ? 'ok' : failures >= (data.threshold || 10) ? 'quarantined' : 'degraded'

  return (
    <>
      <PageHeader
        title={data.node?.displayName}
        badge={<Badge state={state} title={data.node?.lastStatus} />}
        meta={[
          data.selected ? `${data.selected.vendor}${data.selected.version ? ` ${data.selected.version}` : ''}` : data.node?.address,
          data.selected?.clusterName || null,
          `collected ${fmtTime(data.node?.lastCollection ?? '')}`,
        ].filter(Boolean).join(' · ')}
        actions={
          <>
            {/* design/ puts the process picker and the snapshot stamp in the
                title bar, and renders the picker as static text when the node
                runs a single process ("no extra click, no extra page"). */}
            {data.instances.length > 1 ? (
              <Select
                options={data.instances.map((in_) => ({ value: in_.id.toString(), text: `Process: ${in_.displayName}` }))}
                value={data.selected?.id.toString() ?? ''}
                onChange={(v) => setProcess(BigInt(v))}
              />
            ) : data.selected && (
              <span className="m mu">Process: {data.selected.displayName}</span>
            )}
            <span className="m mu">Snapshot: current · {fmtTime(data.snapshot?.capturedAt ?? '')}</span>
            {isAdmin && (
              <>
                <Button small loading={collect.isPending || data.running} onClick={() => collect.mutate()}>
                  {data.running ? 'Collecting…' : 'Collect now'}
                </Button>
                <Button small danger onClick={() => setConfirmDelete(true)}>Remove node</Button>
              </>
            )}
          </>
        }
      />

      {data.running && (
        <div style={{ padding: '8px 16px', background: '#fff', borderBottom: '1px solid #d3dae6' }}>
          <CallOut title="Collection running…" />
        </div>
      )}

      {data.pendingKeys.length > 0 && (
        <div style={{ padding: '10px 16px', background: '#fff', borderBottom: '1px solid #d3dae6' }}>
          <CallOut title={`${data.pendingKeys.length} host key(s) waiting for approval`} color="warning">
            {data.pendingKeys.map((pk) => (
              <div key={pk.key?.id.toString()} className="row" style={{ alignItems: 'center', marginTop: 6 }}>
                <div style={{ flex: 1 }}>{pk.key?.algorithm} {pk.key?.fingerprint} {pk.previous && '(rekey)'}</div>
                {isAdmin && (
                  <>
                    <Button small onClick={() => pk.key && decide.mutate({ id: Number(pk.key.id), decision: 'approve' })}>Approve</Button>
                    <Button small danger onClick={() => pk.key && decide.mutate({ id: Number(pk.key.id), decision: 'reject' })}>Reject</Button>
                  </>
                )}
              </div>
            ))}
          </CallOut>
        </div>
      )}

      <Tabs
        tabs={tabs}
        selected={tab || 'overview'}
        onSelect={(id) => setParams((p) => { if (id === 'overview') p.delete('tab'); else p.set('tab', id); return p })}
      />

      {tab === 'upstreams' && (
        <QueryBar>
          <span className="fld f">
            <span className="m mus">filter</span>
            <input value={tabQ} onChange={(e) => setTabQ(e.target.value)} placeholder="pools or members" />
          </span>
          <Select value={resolution} onChange={setResolution}
            options={[{ value: '', text: 'Resolution: any' },
              ...[...new Set(data.upstreams.map((u) => u.resolution).filter(Boolean))].sort().map((r) => ({ value: r, text: `Resolution: ${r}` }))]} />
          <Select value={poolSort} onChange={setPoolSort}
            options={[{ value: 'members', text: 'Sort: members desc' }, { value: 'name', text: 'Sort: pool name' }]} />
          <FlexSpacer />
          <span className="m mus">
            {data.upstreams.length} pool{data.upstreams.length === 1 ? '' : 's'} · {data.upstreams.reduce((n, u) => n + u.memberCount, 0)} members
          </span>
        </QueryBar>
      )}

      {tab === 'certificates' && (
        <QueryBar>
          <span className="fld f">
            <span className="m mus">filter</span>
            <input value={tabQ} onChange={(e) => setTabQ(e.target.value)} placeholder="subject or path" />
          </span>
          <Select value={expiry} onChange={setExpiry}
            options={[
              { value: '', text: 'Expiry: any' },
              { value: '30', text: 'Expiry: ≤ 30 days' },
              { value: 'expired', text: 'Expiry: expired' },
            ]} />
          <FlexSpacer />
          <span className="m mus">
            {data.certificates.length} binding{data.certificates.length === 1 ? '' : 's'} ·{' '}
            {new Set(data.certificates.map((c) => c.fingerprint)).size} distinct certificates
          </span>
        </QueryBar>
      )}

      {tab === 'routes' && (
        <QueryBar>
          <span className="fld f">
            <span className="m mus">route</span>
            <span className="m mu">{data.selectedSiteName || 'no site'} ›</span>
            <input value={tabQ} onChange={(e) => setTabQ(e.target.value)} placeholder="paths starting /api/" />
          </span>
          <span className="fld">
            <span className="m mus">test a path</span>
            <input
              value={testPath}
              onChange={(e) => setTestPath(e.target.value)}
              placeholder="/api/v2/charge"
              onKeyDown={(e) => {
                if (e.key !== 'Enter' || !data.selectedSiteName) return
                navigate(`/trace?url=${encodeURIComponent(`https://${data.selectedSiteName}${testPath.startsWith('/') ? '' : '/'}${testPath}`)}`)
              }}
            />
          </span>
        </QueryBar>
      )}

      {tab === 'files' && (
        <QueryBar>
          <span className="fld f">
            <span className="m mus">file</span>
            <input value={tabQ} onChange={(e) => setTabQ(e.target.value)} placeholder="path or name" />
          </span>
          <FlexSpacer />
          <span className="m mus">{data.files.length} file{data.files.length === 1 ? '' : 's'}</span>
        </QueryBar>
      )}

      <div className="bd" style={['overview', '', 'routes', 'files'].includes(tab) ? { flexDirection: 'row' } : undefined}>
        {(tab === '' || tab === 'overview') && <OverviewTab data={data} setProcess={setProcess} nodeID={nodeID} isAdmin={isAdmin} />}

        {tab === 'sites' && <SitesTab data={data} />}

        {tab === 'routes' && <RoutesTab data={data} setParams={setParams} filter={tabQ} />}

        {tab === 'upstreams' && <UpstreamsTab data={data} setParams={setParams} filter={tabQ} resolution={resolution} sort={poolSort} />}

        {tab === 'certificates' && <CertificatesTab data={data} filter={tabQ} expiry={expiry} />}

        {tab === 'files' && <ConfigFilesTab data={data} setParams={setParams} filter={tabQ} />}

        {tab === 'drift' && (
          <Panel z>
            <PanelHeader title="Drift findings" />
            {data.noDriftReason ? (
              <div className="m mus" style={{ padding: '2px 12px 8px' }}>{data.noDriftReason}</div>
            ) : (
              <Table<DriftFinding>
                items={data.driftFindings}
                rowKey={(f) => f.id.toString()}
                columns={[
                  { name: 'Object', render: (f) => f.objectKind },
                  { name: 'Key', render: (f) => f.naturalKey },
                  { name: 'Change', render: (f) => f.change },
                  { name: 'Field', render: (f) => f.field },
                ]}
                emptyMessage="No drift findings."
              />
            )}
          </Panel>
        )}
      </div>

      {confirmDelete && (
        <ConfirmModal
          title="Remove this node?"
          body="This removes the node and its collected data. This cannot be undone."
          onCancel={() => setConfirmDelete(false)}
          onConfirm={() => del.mutate(nodeID, { onSuccess: () => navigate('/nodes') })}
          danger
          loading={del.isPending}
          confirmLabel="Remove"
        />
      )}
    </>
  )
}

// A modal rather than an inline edit next to the credential row: the Host
// panel is a fixed 300px sidebar column, too narrow for a select plus
// Save/Cancel without spilling into the panel beside it.
function ChangeCredentialModal({ nodeID, current, credentials, onClose }: {
  nodeID: number
  current: string
  credentials: Credential[]
  onClose: () => void
}) {
  const change = useChangeNodeCredential(nodeID)
  const [picked, setPicked] = useState('')
  return (
    <Modal
      title="Change credential"
      onClose={onClose}
      footer={
        <>
          <Button subtle onClick={onClose}>Cancel</Button>
          <Button
            primary
            disabled={!picked}
            loading={change.isPending}
            onClick={() => change.mutate(Number(picked), { onSuccess: onClose })}
          >
            Save
          </Button>
        </>
      }
    >
      <div className="col" style={{ gap: 8 }}>
        <div className="m mu">Currently {current || 'none assigned'}. This changes which stored credential nagipath uses to connect — it never takes a secret directly.</div>
        <Select
          value={picked}
          onChange={setPicked}
          options={[
            { value: '', text: 'Select a credential' },
            ...credentials.map((c) => ({ value: c.id.toString(), text: `${c.name} · ${c.username} · ${c.authKind}` })),
          ]}
        />
        {change.isError && <p className="m" style={{ color: '#a1231c' }}>{change.error.message}</p>}
      </div>
    </Modal>
  )
}

// design/'s screen 3c: a 300px Host / Processes / Collection column beside
// the five stat tiles and the sites table. Every number here comes from
// GetNode's `stats` and `snapshot`, which the page previously ignored.
// Omitted from 3c: the "bastion" and collection "method"/"duration"/
// "schedule" kv rows (NodeDetailResponse carries none of them — collection
// scheduling lives on Settings › Collection defaults, not per node) and the
// "3 default_server" / "41 regex" stat sub-labels, which NodeStats
// doesn't break out.
function OverviewTab({ data, setProcess, nodeID, isAdmin }: {
  data: NodeDetailResponse
  setProcess: (pid: bigint) => void
  nodeID: number
  isAdmin: boolean
}) {
  const s = data.stats
  const snap = data.snapshot
  const approved = data.hostKeys.find((k) => k.state === 'approved') ?? data.hostKeys[0]
  const [changingCredential, setChangingCredential] = useState(false)

  const credentialValue = (
    <span style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
      {data.credentialName || <span className="mus">none assigned</span>}
      {isAdmin && <Button small subtle onClick={() => setChangingCredential(true)}>Change</Button>}
    </span>
  )

  return (
    <>
      <div className="col" style={{ flex: '0 0 300px' }}>
        <Panel style={{ padding: '10px 12px' }}>
          <div className="lbl" style={{ marginBottom: 6 }}>Host</div>
          <Kv labelWidth={96} rows={[
            ['address', `${data.node?.address}${data.node?.sshPort && data.node.sshPort !== 22 ? `:${data.node.sshPort}` : ''}`],
            ['os', data.node?.osFamily || <span className="mus">unknown</span>],
            ['cluster', data.selected?.clusterName || <span className="mus">unassigned</span>],
            ['credential', credentialValue],
            ['ssh user', data.node?.sshUsername || <span className="mus">—</span>],
            ['host key', approved ? `${approved.algorithm} ${approved.fingerprint}` : <span className="mus">none recorded</span>],
            ['source', data.node?.source || <span className="mus">—</span>],
          ]} />
        </Panel>

        <Panel style={{ padding: '10px 12px' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 6 }}>
            <span className="lbl">Processes</span>
            <span className="m mus" style={{ marginLeft: 'auto' }}>{data.instances.length}</span>
          </div>
          {data.instances.map((in_) => {
            const on = in_.id === data.selected?.id
            return (
              <div
                key={in_.id.toString()}
                className="fct"
                onClick={() => setProcess(in_.id)}
                style={{ cursor: 'pointer', borderRadius: 3, padding: '3px 5px', background: on ? '#e6f1fa' : undefined }}
              >
                <span className={on ? 'm' : 'm mu'}>{in_.displayName}</span>
                <span className="c">{in_.siteCount} sites · {in_.routeCount} routes</span>
              </div>
            )
          })}
          <div className="m mus" style={{ marginTop: 6 }}>tabs below show the selected process</div>
        </Panel>

        <Panel style={{ padding: '10px 12px', flex: 1, minHeight: 0 }}>
          <div className="lbl" style={{ marginBottom: 6 }}>Collection</div>
          <Kv labelWidth={96} rows={[
            ['last run', data.node?.lastCollection ? new Date(data.node.lastCollection).toLocaleString() : <span className="mus">never</span>],
            ['result', data.node?.lastStatus || <span className="mus">—</span>],
            ['failures', data.node?.consecutiveFailures ?? 0],
            ['files parsed', snap ? `${snap.fileCount} · ${(Number(snap.bytesRaw) / 1024).toFixed(0)} KB` : <span className="mus">—</span>],
            ['parse state', snap?.parseState || <span className="mus">—</span>],
          ]} />
          {snap?.degraded && (
            <div className="m mus" style={{ marginTop: 7 }}>degraded: {snap.degradedReason}</div>
          )}
          <div style={{ display: 'flex', gap: 6, marginTop: 8 }}>
            <Button small subtle href="/collections">Collection jobs</Button>
          </div>
        </Panel>
      </div>

      <div className="col" style={{ flex: 1 }}>
        <StatRow stats={[
          { label: 'Sites', value: s?.sites ?? 0 },
          { label: 'Routes', value: s?.routes ?? 0 },
          { label: 'Upstreams', value: s?.upstreams ?? 0 },
          { label: 'Cert bindings', value: s?.certCount ?? 0 },
          { label: 'Drift', value: s?.driftCount ?? 0, sub: data.driftRuns[0]?.baselineLabel || 'no baseline', tone: (s?.driftCount ?? 0) > 0 ? 'warning' : undefined },
        ]} />
        <Panel z style={{ flex: 1 }}>
          <PanelHeader title="Sites on this node" meta={data.selected?.displayName} />
          <SitesTable data={data} />
        </Panel>
      </div>

      {changingCredential && (
        <ChangeCredentialModal
          nodeID={nodeID}
          current={data.credentialName}
          credentials={data.credentials}
          onClose={() => setChangingCredential(false)}
        />
      )}
    </>
  )
}

// Same table on the Overview panel and the Sites tab. design/'s 3c also shows
// per-site Listener / Upstreams / Certificate / Fleet-count / State columns;
// GetNode's `inst.sites` carries only the parsed site graph (names, kind,
// routes), and those five are fleet-wide facts the Sites screen computes — so
// the row links there instead of guessing them here.
function SitesTable({ data }: { data: NodeDetailResponse }) {
  const sites = data.inst?.sites ?? []
  return (
    <>
      <Table<Site>
        items={sites}
        rowKey={(s) => s.id.toString()}
        columns={[
          { name: 'Hostname', render: (s) => <a className="m" href={`/sites/${encodeURIComponent(s.primaryName)}`} style={{ fontWeight: 500 }}>{s.primaryName}</a> },
          { name: 'Kind', width: 110, render: (s) => <span className="m mu">{s.kind || '—'}</span> },
          {
            name: 'Aliases', width: 200,
            render: (s) => s.names.length > 1 ? <span className="m mu">{s.names.filter((n) => n !== s.primaryName).join(', ')}</span> : <span className="m mus">none</span>,
          },
          { name: 'Routes', width: 70, render: (s) => <span className="m">{s.routes.length}</span> },
          { name: 'Fleet', width: 120, render: (s) => <a className="m mu" href={`/sites?site=${encodeURIComponent(s.primaryName)}`}>across fleet ›</a> },
        ]}
        emptyMessage="No sites on this process."
      />
      <PanelFooter>
        <span className="m mus">{sites.length} of {data.stats?.sites ?? sites.length}</span>
      </PanelFooter>
    </>
  )
}

function SitesTab({ data }: { data: NodeDetailResponse }) {
  return (
    <Panel z style={{ flex: 1 }}>
      <PanelHeader title="Sites on this node" meta={data.selected?.displayName} />
      <SitesTable data={data} />
    </Panel>
  )
}

// design/'s screen 2o: the pool table, each pool's members and
// address-resolution summary expanded under its own row. `pool` selects a row
// server-side (GetNode's
// loadUpstreamsTab, which defaults to the first pool) — the page previously
// rendered only `poolMembers`, with no pool list at all. Omitted from 2o: the
// keepalive / health-check / resolver / defined-in kv block and "Group: none",
// none of which UpstreamPool carries.
function UpstreamsTab({ data, setParams, filter, resolution, sort }: {
  data: NodeDetailResponse
  setParams: SetURLSearchParams
  filter: string
  resolution: string
  sort: string
}) {
  const pools = data.upstreams
    .filter((p) => !filter || p.name.toLowerCase().includes(filter.toLowerCase()))
    .filter((p) => !resolution || p.resolution === resolution)
    .sort((a, b) => (sort === 'name' ? a.name.localeCompare(b.name) : b.memberCount - a.memberCount))
  const members = data.poolMembers
  const inInventory = members.filter((m) => m.nodeName).length
  const named = members.filter((m) => !m.nodeName && !/^[\d.:a-f]+$/i.test(m.host)).length

  return (
    <Panel z style={{ flex: 1 }}>
      <PanelHeader
        title="Pools"
        meta={`${data.upstreams.length} pool${data.upstreams.length === 1 ? '' : 's'} · expand a pool to see its members`}
      />
      <Table<UpstreamPool>
        items={pools}
        rowKey={(p) => p.id.toString()}
        rowClassName={(p) => (p.id === data.selectedPool?.id ? 'hl' : undefined)}
        onRowClick={(p) => setParams((prev) => {
          if (p.id === data.selectedPool?.id) prev.delete('pool'); else prev.set('pool', p.id.toString())
          return prev
        })}
        columns={[
          {
            name: 'Pool', width: 150,
            render: (p) => <span className="m" style={{ fontWeight: 500 }}><Disclosure open={p.id === data.selectedPool?.id} />{p.name}</span>,
          },
          { name: 'Members', width: 70, render: (p) => <span className="m">{p.memberCount}</span> },
          { name: 'Balance', width: 110, render: (p) => <span className="m mu">{p.balanceMethod || '—'}</span> },
          { name: 'Used by', render: (p) => <span className="m mu">{p.usedBy || '—'}</span> },
          { name: 'Resolution', width: 145, render: (p) => <span className="m mu">{p.resolution || '—'}</span> },
          { name: 'State', width: 110, render: (p) => <Badge state={p.state.toLowerCase()}>{p.state}</Badge> },
        ]}
        emptyMessage={filter ? 'No pool matches this filter.' : 'No upstream pools on this process.'}
        renderExpanded={(p) => {
          // GetNode hydrates poolMembers only for ?pool=, so exactly one row
          // can be open at a time.
          if (p.id !== data.selectedPool?.id) return null
          return (
            <div className="row" style={{ alignItems: 'flex-start' }}>
              <div className="col" style={{ flex: 1, gap: 4 }}>
                <span className="lbl">members · {p.memberCount}</span>
                <Table<UpstreamMember>
                  items={members}
                  rowKey={(m) => `${m.host}:${m.port}:${m.role}:${m.nodeName}`}
                  columns={[
                    { name: 'Member', render: (m) => <span className="m">{m.host}:{m.port}</span> },
                    { name: 'Weight', width: 56, render: (m) => <span className="m">{m.weight || '—'}</span> },
                    { name: 'Role', width: 70, render: (m) => <span className="m mu">{m.role || '—'}</span> },
                    { name: 'Maps to', width: 112, render: (m) => m.nodeName ? <span className="m mu">{m.nodeName}</span> : <span className="m mus">outside</span> },
                  ]}
                  emptyMessage="No members in this pool."
                />
              </div>

              <div className="col" style={{ flex: '0 0 320px', gap: 4 }}>
                <span className="lbl">address resolution</span>
                <Facet label={<><Badge cls="v">{inInventory}</Badge> match a Node in inventory</>} />
                <Facet label={<><Badge cls="e">{members.length - inInventory - named}</Badge> outside inventory</>} />
                <Facet label={<><Badge cls="i">{named}</Badge> DNS name, not resolved at collection</>} />
                <div className="m mus">Resolution is done on the target node, at collection time.</div>
              </div>
            </div>
          )
        }}
      />
    </Panel>
  )
}

// design/'s screen 2p: the node's certificate bindings, each binding's detail
// expanded under its own row. Omitted from 2p: the SAN/issuer/valid-from kv rows
// and the "Also bound on" fleet list — NodeCertBinding carries the fingerprint
// but no SANs, issuer, validity window or file paths, and the fleet-wide
// binding list is exactly what the Certificates screen is, so the panel links
// there rather than half-reproducing it.
function CertificatesTab({ data, filter, expiry }: { data: NodeDetailResponse; filter: string; expiry: string }) {
  const [selected, setSelected] = useState<string | null>(null)
  const certs = data.certificates
    .filter((c) => !filter || `${c.subjectCn} ${c.siteName}`.toLowerCase().includes(filter.toLowerCase()))
    .filter((c) => {
      if (!expiry) return true
      const days = daysUntil(c.notAfter)
      return expiry === 'expired' ? days <= 0 : days <= 30
    })
  const key = (c: NodeCertBinding) => `${c.fingerprint}:${c.siteName}:${c.port}`
  const distinct = new Set(data.certificates.map((c) => c.fingerprint)).size

  return (
    <Panel z style={{ flex: 1 }}>
      <PanelHeader
        title="Certificate bindings on this node"
        meta={`${data.certificates.length} binding${data.certificates.length === 1 ? '' : 's'} · ${distinct} distinct certificate${distinct === 1 ? '' : 's'} · expand a binding for its detail`}
      />
      <Table<NodeCertBinding>
        items={certs}
        rowKey={key}
        rowClassName={(c) => (key(c) === selected ? 'hl' : undefined)}
        onRowClick={(c) => setSelected((s) => (s === key(c) ? null : key(c)))}
        columns={[
          {
            name: 'Subject',
            render: (c) => <span className="m" style={{ fontWeight: 500 }}><Disclosure open={key(c) === selected} />{c.subjectCn || '(no CN)'}</span>,
          },
          { name: 'Bound to', width: 170, render: (c) => <span className="m mu">{c.siteName || '—'}{c.port > 0 ? `:${c.port}` : ''}</span> },
          { name: 'Expires', width: 100, render: (c) => <span className="m mu">{c.notAfter ? new Date(c.notAfter).toLocaleDateString() : '—'}</span> },
          { name: 'Key', width: 96, render: (c) => <span className="m mu">{c.keyType || '—'}</span> },
          { name: 'Chain', width: 80, render: (c) => <span className="m mu">{c.chainLength > 0 ? `${c.chainLength} interm.` : 'internal'}</span> },
          { name: 'State', width: 100, render: (c) => expiryBadge(c.notAfter) },
        ]}
        emptyMessage={filter ? 'No binding matches this filter.' : 'No certificates on this process.'}
        renderExpanded={(c) => key(c) !== selected ? null : (
          <div className="row" style={{ alignItems: 'flex-start' }}>
            <div className="col" style={{ flex: '0 0 380px', gap: 8 }}>
              <Kv labelWidth={82} rows={[
                ['SHA-256', c.fingerprint || <span className="mus">—</span>],
                ['bound to', `${c.siteName || '—'}${c.port > 0 ? `:${c.port}` : ''}`],
                ['expires', c.notAfter ? new Date(c.notAfter).toLocaleDateString() : <span className="mus">—</span>],
                ['key', c.keyType || <span className="mus">—</span>],
                ['chain', c.chainLength > 0 ? `${c.chainLength} intermediate(s)` : 'internal'],
              ]} />
              <div style={{ display: 'flex', gap: 6 }}>
                <Button small subtle href={`/certificates?q=${encodeURIComponent(c.subjectCn)}`}>Fleet-wide bindings</Button>
              </div>
            </div>
          </div>
        )}
      />
    </Panel>
  )
}

// design/'s 5a wireframe: a 3-pane cascade (site -> route -> effect), each
// pane server-resolved rather than recomputed client-side — `site`/`route`
// query params (matched here to GetNodeRequest.site/route in
// internal/api/nodeservice.go, which already implements this resolution;
// the frontend just wasn't sending them) drive `selectedRoute`/`routeEffect`
// same as `process`/`file` drive the other tabs. The two list panes and
// their active/inactive row styling are lifted from 5a's literal markup
// (inline `background:#0077cc` on the selected row — there's no reusable
// primitive for this list-cascade shape yet, so it's built directly here).
function RoutesTab({ data, setParams, filter }: { data: NodeDetailResponse; setParams: SetURLSearchParams; filter: string }) {
  const sites = data.inst?.sites ?? []
  const selectedSite = sites.find((s) => s.primaryName === data.selectedSiteName)
  const routes = (selectedSite?.routes ?? []).filter((r) => !filter || r.pattern.toLowerCase().includes(filter.toLowerCase()))

  return (
    <div className="pnl z" style={{ flex: 1, overflow: 'hidden' }}>
      <div style={{ flex: 1, minHeight: 0, display: 'flex' }}>
        <div style={{ flex: '0 0 216px', minWidth: 0, borderRight: '1px solid #d3dae6', display: 'flex', flexDirection: 'column' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 6, padding: '6px 9px', borderBottom: '1px solid #edf0f5', background: '#f7f8fc' }}>
            <span className="lbl">Site</span><span className="m mus" style={{ marginLeft: 'auto' }}>{sites.length}</span>
          </div>
          <div style={{ flex: 1, minHeight: 0, overflow: 'auto', padding: '4px 5px', display: 'flex', flexDirection: 'column', gap: 1 }}>
            {sites.map((s) => {
              const active = s.primaryName === data.selectedSiteName
              return (
                <div
                  key={s.id.toString()}
                  onClick={() => setParams((p) => { p.set('site', s.primaryName); p.delete('route'); return p })}
                  style={{ display: 'flex', flexDirection: 'column', gap: 1, padding: '4px 7px', borderRadius: 3, cursor: 'pointer', background: active ? '#0077cc' : undefined, color: active ? '#fff' : undefined }}
                >
                  <div style={{ display: 'flex', alignItems: 'baseline', gap: 6 }}>
                    <span className="m" style={{ flex: 1, minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{s.primaryName}</span>
                    {active && <span className="m" style={{ opacity: .8 }}>›</span>}
                  </div>
                  <span className="m" style={{ opacity: .65, fontSize: 10 }}>{s.routes.length}</span>
                </div>
              )
            })}
          </div>
        </div>

        <div style={{ flex: '0 0 244px', minWidth: 0, borderRight: '1px solid #d3dae6', display: 'flex', flexDirection: 'column' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 6, padding: '6px 9px', borderBottom: '1px solid #edf0f5', background: '#f7f8fc' }}>
            <span className="lbl">Route</span><span className="m mus" style={{ marginLeft: 'auto' }}>{routes.length}</span>
          </div>
          <div style={{ flex: 1, minHeight: 0, overflow: 'auto', padding: '4px 5px', display: 'flex', flexDirection: 'column', gap: 1 }}>
            {routes.length === 0 && <div className="m mus" style={{ padding: '6px 7px' }}>No routes on this site.</div>}
            {routes.map((r) => {
              const active = r.pattern === data.selectedRoute?.pattern
              return (
                <div
                  key={r.id.toString()}
                  onClick={() => setParams((p) => { p.set('route', r.pattern); return p })}
                  style={{ display: 'flex', flexDirection: 'column', gap: 1, padding: '4px 7px', borderRadius: 3, cursor: 'pointer', background: active ? '#0077cc' : undefined, color: active ? '#fff' : undefined }}
                >
                  <div style={{ display: 'flex', alignItems: 'baseline', gap: 6 }}>
                    <span className="m" style={{ flex: 1, minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{r.matchType} {r.pattern}</span>
                    {active && <span className="m" style={{ opacity: .8 }}>›</span>}
                  </div>
                </div>
              )
            })}
          </div>
        </div>

        <div style={{ flex: 1, minWidth: 0, display: 'flex', flexDirection: 'column' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '7px 10px', borderBottom: '1px solid #edf0f5', background: '#f7f8fc' }}>
            <span className="lbl">Effect</span>
            {data.selectedRoute && <span className="m mus">{data.selectedRoute.pattern}</span>}
          </div>
          <div style={{ flex: 1, minHeight: 0, padding: '10px 12px', overflow: 'auto' }}>
            {data.selectedRoute ? (
              <div className="kv" style={{ display: 'grid', gridTemplateColumns: '104px 1fr', gap: '7px 10px', alignItems: 'start' }}>
                <span className="lbl">Match</span><span className="m">{data.selectedRoute.matchType} {data.selectedRoute.pattern}</span>
                <span className="lbl">Target</span><span className="m">{data.selectedRoute.targetRaw}</span>
                {data.routeEffect ? (
                  <>
                    <span className="lbl">Upstream</span>
                    <span className="m">
                      {data.routeEffect.upstreamName} <span className="m mu">· {data.routeEffect.balanceMethod} · {data.routeEffect.memberCount} member(s)</span>
                    </span>
                    <span className="lbl">Members</span>
                    <span>
                      {data.routeEffect.members.map((m) => (
                        <div key={m.id.toString()} className="m">→ {m.host}:{m.port}</div>
                      ))}
                    </span>
                  </>
                ) : (
                  <>
                    <span className="lbl">Upstream</span><span className="m mus">terminal — no upstream to resolve</span>
                  </>
                )}
              </div>
            ) : (
              <div className="m mus">No route selected.</div>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}

function fmtBytes(n: bigint): string {
  const v = Number(n)
  return v < 1024 ? `${v} B` : `${(v / 1024).toFixed(1)} KB`
}

// design/'s 5b wireframe groups files by *include graph* (what actually
// pulls in what); our snapshot only records a flat file list, so this groups
// by directory instead — the closest tree shape available without a backend
// change to record include edges at collection time. Selected-row styling
// and the line-numbered viewer are lifted from 5b's literal markup; the
// numbered lines themselves use CodeBlock (same `.code`/`.cl`/`.no`
// classes 5b's own `<pre>`-free code block uses), seeded at the snapshot's
// recorded `line_start` rather than always starting at 1, since the backend
// already resolves that offset for exactly this view.
function ConfigFilesTab({ data, setParams, filter }: { data: NodeDetailResponse; setParams: SetURLSearchParams; filter: string }) {
  const byDir = new Map<string, FileRef[]>()
  for (const f of data.files.filter((f) => !filter || f.path.toLowerCase().includes(filter.toLowerCase()))) {
    const dir = f.path.includes('/') ? f.path.slice(0, f.path.lastIndexOf('/')) : '/'
    if (!byDir.has(dir)) byDir.set(dir, [])
    byDir.get(dir)!.push(f)
  }
  const lines = data.fileBody ? data.fileBody.split('\n') : []

  return (
    <div className="pnl z" style={{ flex: 1, overflow: 'hidden' }}>
      <div style={{ flex: 1, minHeight: 0, display: 'flex' }}>
        <div style={{ flex: '0 0 260px', minWidth: 0, borderRight: '1px solid #d3dae6', display: 'flex', flexDirection: 'column' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 6, padding: '6px 9px', borderBottom: '1px solid #edf0f5', background: '#f7f8fc' }}>
            <span className="lbl">Files</span><span className="m mus" style={{ marginLeft: 'auto' }}>{data.files.length}</span>
          </div>
          <div style={{ flex: 1, minHeight: 0, overflow: 'auto', padding: '5px 7px', display: 'flex', flexDirection: 'column', gap: 1 }}>
            {data.files.length === 0 && <div className="m mus" style={{ padding: '4px 6px' }}>No files in this snapshot.</div>}
            {[...byDir.entries()].map(([dir, files]) => (
              <div key={dir}>
                <div className="m mus" style={{ padding: '4px 6px 2px' }}>{dir}</div>
                {files.map((f) => {
                  const active = data.selectedFile?.id === f.id
                  return (
                    <div
                      key={f.id.toString()}
                      onClick={() => setParams((p) => { p.set('file', f.id.toString()); return p })}
                      style={{ display: 'flex', alignItems: 'center', gap: 6, padding: '2px 6px 2px 18px', borderRadius: 3, cursor: 'pointer', background: active ? '#0077cc' : undefined, color: active ? '#fff' : undefined }}
                    >
                      <span className="m" style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', fontWeight: active ? 500 : undefined }}>
                        {f.path.split('/').pop()}
                      </span>
                      <span className="m" style={{ marginLeft: 'auto', opacity: .6, fontSize: 10.5, flex: 'none' }}>{fmtBytes(f.bytesRaw)}</span>
                      {f.truncated && <Badge cls="d">TRUNCATED</Badge>}
                    </div>
                  )
                })}
              </div>
            ))}
          </div>
        </div>

        <div style={{ flex: 1, minWidth: 0, display: 'flex', flexDirection: 'column' }}>
          {data.selectedFile ? (
            <>
              <div style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '6px 10px', borderBottom: '1px solid #edf0f5', background: '#f7f8fc' }}>
                <span className="m" style={{ fontWeight: 600 }}>{data.selectedFile.path}</span>
                <span className="m mus">{fmtBytes(data.selectedFile.bytesRaw)}</span>
              </div>
              <div style={{ flex: 1, minHeight: 0, padding: '10px 12px', overflow: 'auto' }}>
                <CodeBlock lines={lines} startLine={data.lineStart || 1} />
              </div>
            </>
          ) : (
            <div className="m mus" style={{ padding: '10px 12px' }}>Select a file to view it.</div>
          )}
        </div>
      </div>
    </div>
  )
}
