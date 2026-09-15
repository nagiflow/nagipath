import { useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useSession } from '../../api/queries/session'
import { useDecideHostKey, useHostKeys, useSetHostKeyPolicy } from '../../api/queries/settings'
import type { PendingHostKey } from '../../api/pb/nagipath/api/v1/nodes_pb'
import { PanelHeader } from '../../components/shared/PanelHeader'
import {
  Badge, Button, Loading, Panel, PanelFooter, Radio, Select, StatRow, Table,
  type Column,
} from '../../components/ui'
import { SettingsLayout } from './SettingsLayout'

// A pending key that replaces an already-approved one for the same algorithm is
// a different situation from a first sighting: it is a rekey or an interception,
// and store.AllHostKeys marks it by returning the approved fingerprint as
// `previous`. The store keeps both in state 'pending', so the distinction is
// drawn here.
function isChanged(k: PendingHostKey): boolean {
  return k.key?.state === 'pending' && k.previous !== ''
}

function stateBadge(k: PendingHostKey) {
  if (isChanged(k)) return <Badge cls="r">CHANGED</Badge>
  return <Badge state={k.key?.state ?? ''} />
}

function ts(s: string): string {
  return s ? new Date(s).toLocaleString() : '—'
}

// A full SHA256 fingerprint is 50 characters and .t is not table-layout:fixed,
// so an untruncated column pushes State off the panel. design/ elides the middle
// the same way; the expanded row carries the whole value.
function fp(s: string): string {
  return s.length > 26 ? `${s.slice(0, 18)}…${s.slice(-4)}` : s
}

// Ported from internal/web/templates/hostkeys.html against
// GET /api/settings/hostkeys and POST /api/hostkeys/{id}/decide
// (internal/api/settingsservice.go), laid out as design/'s screen 8b: the four
// stat tiles, the State/Cluster-filtered table whose ticked rows expand into
// what each key presented, and the trust policy.
// Selection drives the footer's Approve/Reject the way design/ does; each
// decision is one POST to the per-row endpoint, looped.
// Omitted from that screen: "Import known_hosts" (nothing parses one — every key
// nagipath trusts was presented by the host and approved by a human) and the
// "of 428 nodes" figure under Approved
// (HostKeyStats counts keys, not the nodes they cover), the "Collect after
// approval" action (approval does not queue a collection), "approved <date> by
// <user>" on the recorded key (the decision is audited, but AllHostKeys returns
// only the sibling's fingerprint), and the pager (this endpoint returns every
// key that matches the filter — the fleet has one key per algorithm per node).
export function HostKeysPage() {
  const { data: session } = useSession()
  const isAdmin = session?.user?.role === 'admin'
  const [params, setParams] = useSearchParams()
  const state = params.get('state') ?? ''
  const cluster = params.get('cluster') ?? ''
  const { data, isPending, isError, error } = useHostKeys(state, cluster)
  const decide = useDecideHostKey()
  const setPolicy = useSetHostKeyPolicy()
  const [picked, setPicked] = useState<Set<string>>(new Set())
  const [busy, setBusy] = useState(false)
  const [bulkError, setBulkError] = useState('')

  const keys = data?.keys ?? []
  const selected = keys.filter((k) => picked.has(String(k.key?.id)))
  const algorithms = Object.entries(data?.stats?.algorithms ?? {})

  const toggle = (id: string) => setPicked((cur) => {
    const next = new Set(cur)
    if (next.has(id)) next.delete(id); else next.add(id)
    return next
  })

  async function apply(approve: boolean) {
    const ids = selected.filter((k) => k.key?.state === 'pending').map((k) => Number(k.key!.id))
    setBusy(true)
    setBulkError('')
    const results = await Promise.allSettled(ids.map((id) => decide.mutateAsync({ id, approve })))
    setBusy(false)
    setPicked(new Set())
    const failed = results.filter((r) => r.status === 'rejected').length
    if (failed > 0) setBulkError(`${failed} of ${ids.length} host key(s) could not be decided.`)
  }

  const columns: Column<PendingHostKey>[] = [
    {
      name: '',
      width: 26,
      render: (k) => <input type="checkbox" className="cb" checked={picked.has(String(k.key?.id))} readOnly tabIndex={-1} />,
    },
    {
      name: 'Node',
      width: 210,
      render: (k) => <span className="m" style={isChanged(k) ? { fontWeight: 500 } : undefined}>{k.nodeName}</span>,
    },
    { name: 'Algorithm', width: 110, render: (k) => <span className="m mu">{k.key?.algorithm}</span> },
    {
      name: 'Fingerprint',
      render: (k) => <span className="m mu" title={k.key?.fingerprint}>{fp(k.key?.fingerprint ?? '')}</span>,
    },
    { name: 'First seen', width: 130, render: (k) => <span className="m mu">{ts(k.key?.firstSeenAt ?? '')}</span> },
    { name: 'Cluster', width: 120, render: (k) => <span className="m mu">{k.cluster || '—'}</span> },
    { name: 'State', width: 120, render: stateBadge },
  ]

  return (
    <SettingsLayout
      title="Host keys"
      actions={<span className="m mus">{data?.stats?.pending ?? 0} awaiting approval</span>}
    >
      <div className="col">
        {isPending && <Loading label="Loading host keys…" />}
        {isError && <div className="m" style={{ color: '#a1231c' }}>{error.message}</div>}

        {data?.stats && (
          <StatRow
            stats={[
              { label: 'Awaiting approval', value: data.stats.pending, sub: 'blocks collection', tone: 'warning' },
              { label: 'Changed', value: data.stats.changed, sub: 'key differs from record', tone: 'danger' },
              { label: 'Approved', value: data.stats.approved, sub: 'trusted for collection' },
              {
                label: 'Algorithms',
                value: algorithms.length,
                sub: algorithms.map(([a, n]) => `${a} ${n}`).join(' · ') || 'none recorded',
              },
            ]}
          />
        )}

        {data && (
          <Panel z>
            <PanelHeader
              title="Host keys"
              meta="expand a key to compare presented vs. recorded"
              actions={
                <>
                  <Select
                    options={[
                      { value: '', text: 'State: any' },
                      { value: 'pending', text: 'State: pending' },
                      { value: 'changed', text: 'State: changed' },
                      { value: 'approved', text: 'State: approved' },
                    ]}
                    value={state}
                    onChange={(v) => setParams((p) => { v ? p.set('state', v) : p.delete('state'); return p })}
                  />
                  <Select
                    options={[{ value: '', text: 'Cluster: all' },
                      ...(data.clusters ?? []).map((c) => ({ value: c, text: `Cluster: ${c}` }))]}
                    value={cluster}
                    onChange={(v) => setParams((p) => { v ? p.set('cluster', v) : p.delete('cluster'); return p })}
                  />
                </>
              }
            />
            <Table
              items={keys}
              columns={columns}
              rowKey={(k) => String(k.key?.id)}
              rowClassName={(k) => (picked.has(String(k.key?.id)) ? 'hl' : '')}
              onRowClick={(k) => toggle(String(k.key?.id))}
              emptyMessage="No host keys match this filter."
              renderExpanded={(k) => picked.has(String(k.key?.id)) && (
                <div className="kv m" style={{ display: 'grid', gridTemplateColumns: '92px 1fr', gap: '4px 8px' }}>
                  {k.previous && <><span className="mus">recorded</span><span>{k.previous} · approved</span></>}
                  <span className="mus">presented</span>
                  <span>{k.key?.fingerprint} · {k.key?.algorithm} · seen {ts(k.key?.firstSeenAt ?? '')}</span>
                  <span className="mus">address</span><span>{k.nodeAddress || '—'}</span>
                  <span className="mus">collection</span>
                  <span>
                    {k.key?.state === 'approved'
                      ? 'allowed — this key is trusted'
                      : `refused since ${ts(k.key?.firstSeenAt ?? '')}`}
                  </span>
                </div>
              )}
            />
            <PanelFooter>
              <span className="m mus">{selected.length} selected</span>
              {isAdmin && selected.some((k) => k.key?.state === 'pending') && (
                <>
                  <Button primary small loading={busy} onClick={() => apply(true)}>Approve</Button>
                  <Button small loading={busy} onClick={() => apply(false)}>Reject</Button>
                </>
              )}
              {bulkError && <span className="m" style={{ color: '#a1231c' }}>{bulkError}</span>}
              <div style={{ flex: 1 }} />
              <span className="m mus">{keys.length} key{keys.length === 1 ? '' : 's'}</span>
            </PanelFooter>
          </Panel>
        )}

        {data && (
          <Panel>
            <div className="lbl" style={{ marginBottom: 7 }}>Policy</div>
            {isAdmin ? (
              <>
                <div className="fct">
                  <Radio name="hostkey-policy" checked={!data.tofuEnabled} onChange={() => setPolicy.mutate(false)} label="Approve manually" />
                </div>
                <div className="fct">
                  <Radio name="hostkey-policy" checked={data.tofuEnabled} onChange={() => setPolicy.mutate(true)} label="Trust on first use" />
                </div>
              </>
            ) : (
              <div className="fct"><span className="m">{data.tofuEnabled ? 'Trust on first use' : 'Approve manually'}</span></div>
            )}
            <div className="fct"><input type="checkbox" className="cb" checked readOnly disabled /><span className="m">Refuse collection on key change</span></div>
            {setPolicy.isError && <div className="m" style={{ color: '#a1231c', marginTop: 4 }}>{setPolicy.error.message}</div>}
            <div className="m mus" style={{ marginTop: 7 }}>
              {data.tofuEnabled
                ? "A node's first-ever key is trusted automatically. "
                : 'A host key is trusted only once a human approves the fingerprint it presented. '}
              A key that replaces an approved one is always treated as a rekey and stops collection until someone decides which it is — that part never changes.
            </div>
          </Panel>
        )}
      </div>
    </SettingsLayout>
  )
}
