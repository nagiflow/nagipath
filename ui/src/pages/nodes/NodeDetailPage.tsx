import { useState } from 'react'
import {
  EuiBasicTable,
  EuiButton,
  EuiCallOut,
  EuiConfirmModal,
  EuiFlexGroup,
  EuiFlexItem,
  EuiLoadingChart,
  EuiPageTemplate,
  EuiPanel,
  EuiSelect,
  EuiSpacer,
  EuiTabbedContent,
  EuiText,
  EuiTitle,
} from '@elastic/eui'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { APIError } from '../../api/client'
import { useCollectNode, useDecideHostKey, useDeleteNode, useNode } from '../../api/queries/nodes'
import { useSession } from '../../api/queries/session'
import type { DriftFinding, FileRef, NodeCertBinding, UpstreamMember } from '../../api/pb/nagipath/api/v1/nodes_pb'

// Ported from internal/web/templates/node.html against
// GET /api/ui/nodes/{id}[/{tab}] (internal/api/nodes.go) — same tabs, same
// process picker, same running-collection self-refresh (now a TanStack Query
// refetchInterval instead of htmx's `hx-trigger="every 3s"`, see
// api/queries/nodes.ts's useNode). Host-key approve/reject moved to
// POST /api/ui/hostkeys/{id}/decide.
export function NodeDetailPage() {
  const { id = '' } = useParams()
  const nodeID = Number(id)
  const [params, setParams] = useSearchParams()
  const navigate = useNavigate()
  const { data: session } = useSession()
  const tab = params.get('tab') ?? ''
  const process = params.get('process') ? Number(params.get('process')) : undefined
  const file = params.get('file') ? Number(params.get('file')) : undefined

  const { data, isPending, isError, error } = useNode(nodeID, { tab, process, file })
  const collect = useCollectNode(nodeID)
  const del = useDeleteNode()
  const decide = useDecideHostKey()
  const [confirmDelete, setConfirmDelete] = useState(false)

  if (isPending) return <EuiPageTemplate.EmptyPrompt icon={<EuiLoadingChart size="xl" />} title={<h2>Loading node…</h2>} />
  if (isError) {
    const notFound = error instanceof APIError && error.status === 404
    return (
      <EuiPageTemplate.EmptyPrompt
        iconType={notFound ? 'search' : 'alert'}
        color={notFound ? 'subdued' : 'danger'}
        title={<h2>{notFound ? 'No such node' : 'Could not load this node'}</h2>}
        body={<p>{error.message}</p>}
      />
    )
  }

  const isAdmin = session?.user?.role === 'admin'
  const setProcess = (pid: bigint) => setParams((p) => { p.set('process', pid.toString()); return p })

  return (
    <>
      <EuiFlexGroup alignItems="center" justifyContent="spaceBetween">
        <EuiFlexItem grow={false}>
          <EuiTitle size="m"><h1>{data.node?.displayName}</h1></EuiTitle>
          <EuiText size="s" color="subdued">{data.node?.address}</EuiText>
        </EuiFlexItem>
        {isAdmin && (
          <EuiFlexItem grow={false}>
            <EuiFlexGroup gutterSize="s">
              <EuiFlexItem grow={false}>
                <EuiButton size="s" isLoading={collect.isPending || data.running} onClick={() => collect.mutate()}>
                  {data.running ? 'Collecting…' : 'Collect now'}
                </EuiButton>
              </EuiFlexItem>
              <EuiFlexItem grow={false}>
                <EuiButton size="s" color="danger" onClick={() => setConfirmDelete(true)}>Remove node</EuiButton>
              </EuiFlexItem>
            </EuiFlexGroup>
          </EuiFlexItem>
        )}
      </EuiFlexGroup>

      {data.running && (
        <>
          <EuiSpacer size="s" />
          <EuiCallOut size="s" title="Collection running…" iconType="clock" />
        </>
      )}

      {data.instances.length > 1 && (
        <>
          <EuiSpacer size="s" />
          <EuiSelect
            compressed
            options={data.instances.map((in_) => ({ value: in_.id.toString(), text: `${in_.vendor} — ${in_.displayName}` }))}
            value={data.selected?.id.toString() ?? ''}
            onChange={(e) => setProcess(BigInt(e.target.value))}
          />
        </>
      )}

      <EuiSpacer />

      {data.pendingKeys.length > 0 && (
        <>
          <EuiCallOut title={`${data.pendingKeys.length} host key(s) waiting for approval`} color="warning" iconType="alert">
            {data.pendingKeys.map((pk) => (
              <EuiFlexGroup key={pk.key?.id.toString()} alignItems="center" gutterSize="s" style={{ marginTop: 6 }}>
                <EuiFlexItem>
                  <EuiText size="s">{pk.key?.algorithm} {pk.key?.fingerprint} {pk.previous && '(rekey)'}</EuiText>
                </EuiFlexItem>
                {isAdmin && (
                  <>
                    <EuiFlexItem grow={false}>
                      <EuiButton size="s" onClick={() => pk.key && decide.mutate({ id: Number(pk.key.id), decision: 'approve' })}>Approve</EuiButton>
                    </EuiFlexItem>
                    <EuiFlexItem grow={false}>
                      <EuiButton size="s" color="danger" onClick={() => pk.key && decide.mutate({ id: Number(pk.key.id), decision: 'reject' })}>Reject</EuiButton>
                    </EuiFlexItem>
                  </>
                )}
              </EuiFlexGroup>
            ))}
          </EuiCallOut>
          <EuiSpacer />
        </>
      )}

      <EuiTabbedContent
        selectedTab={{ id: tab || 'overview', name: '', content: null }}
        onTabClick={(t) => setParams((p) => { if (t.id === 'overview') p.delete('tab'); else p.set('tab', t.id); return p })}
        tabs={data.tabs.map((t) => ({
          id: t.href.split('/').pop()?.split('?')[0] || 'overview',
          name: t.count > 0 ? `${t.label} (${t.count})` : t.label,
          content: null,
        }))}
      />
      <EuiSpacer />

      {(tab === '' || tab === 'overview') && (
        <EuiPanel>
          <EuiText size="s">
            <p>OS: {data.node?.osFamily || 'unknown'}</p>
            <p>Credential: {data.credentialName || 'none assigned'}</p>
            <p>Consecutive failures: {data.node?.consecutiveFailures}</p>
            <p>Last collection: {data.node?.lastCollection ? new Date(data.node.lastCollection).toLocaleString() : 'never'}</p>
          </EuiText>
        </EuiPanel>
      )}

      {tab === 'sites' && (
        <EuiPanel>
          {(data.inst?.sites ?? []).map((site) => (
            <div key={site.id.toString()} style={{ marginBottom: 12 }}>
              <EuiText size="s"><strong>{site.primaryName}</strong> — {site.routes.length} route(s)</EuiText>
            </div>
          ))}
          {(data.inst?.sites ?? []).length === 0 && <EuiText size="s" color="subdued">No sites on this process.</EuiText>}
        </EuiPanel>
      )}

      {tab === 'routes' && (
        <EuiFlexGroup>
          <EuiFlexItem grow={false} style={{ width: 220 }}>
            <EuiPanel>
              {(data.inst?.sites ?? []).map((site) => (
                <div key={site.id.toString()}>
                  <EuiButton
                    size="s"
                    color={site.primaryName === data.selectedSiteName ? 'primary' : 'text'}
                    onClick={() => setParams((p) => { p.set('site', site.primaryName); return p })}
                  >
                    {site.primaryName}
                  </EuiButton>
                </div>
              ))}
            </EuiPanel>
          </EuiFlexItem>
          <EuiFlexItem>
            <EuiPanel>
              {data.selectedRoute ? (
                <EuiText size="s">
                  <p><strong>{data.selectedRoute.matchType}</strong> {data.selectedRoute.pattern}</p>
                  <p>target: {data.selectedRoute.targetRaw}</p>
                  {data.routeEffect && (
                    <>
                      <p>upstream: {data.routeEffect.upstreamName} ({data.routeEffect.balanceMethod}, {data.routeEffect.memberCount} member(s))</p>
                      {data.routeEffect.members.map((m) => (
                        <p key={m.id.toString()} style={{ marginLeft: 16 }}>→ {m.host}:{m.port}</p>
                      ))}
                    </>
                  )}
                </EuiText>
              ) : (
                <EuiText size="s" color="subdued">No route selected.</EuiText>
              )}
            </EuiPanel>
          </EuiFlexItem>
        </EuiFlexGroup>
      )}

      {tab === 'upstreams' && (
        <EuiPanel>
          <EuiBasicTable<UpstreamMember>
            items={data.poolMembers}
            columns={[
              { field: 'host', name: 'Host' },
              { field: 'port', name: 'Port' },
              { field: 'weight', name: 'Weight' },
              { field: 'role', name: 'Role' },
            ]}
            noItemsMessage="No upstream selected."
          />
        </EuiPanel>
      )}

      {tab === 'certificates' && (
        <EuiPanel>
          <EuiBasicTable<NodeCertBinding>
            items={data.certificates}
            columns={[
              { field: 'subjectCn', name: 'Subject' },
              { field: 'siteName', name: 'Site' },
              { field: 'notAfter', name: 'Expires', render: (v: string) => (v ? new Date(v).toLocaleDateString() : '—') },
              { field: 'keyType', name: 'Key' },
            ]}
            noItemsMessage="No certificates on this process."
          />
        </EuiPanel>
      )}

      {tab === 'files' && (
        <EuiFlexGroup>
          <EuiFlexItem grow={false} style={{ width: 320 }}>
            <EuiPanel>
              <EuiBasicTable<FileRef>
                items={data.files}
                columns={[
                  {
                    field: 'path',
                    name: 'File',
                    render: (path: string, row: FileRef) => (
                      <a href="#" onClick={(e) => { e.preventDefault(); setParams((p) => { p.set('file', row.id.toString()); return p }) }}>
                        {path}
                      </a>
                    ),
                  },
                ]}
                noItemsMessage="No files in this snapshot."
              />
            </EuiPanel>
          </EuiFlexItem>
          <EuiFlexItem>
            <EuiPanel>
              {data.selectedFile ? (
                <pre style={{ whiteSpace: 'pre-wrap', fontSize: 12, margin: 0 }}>{data.fileBody}</pre>
              ) : (
                <EuiText size="s" color="subdued">Select a file to view it.</EuiText>
              )}
            </EuiPanel>
          </EuiFlexItem>
        </EuiFlexGroup>
      )}

      {tab === 'drift' && (
        <EuiPanel>
          {data.noDriftReason ? (
            <EuiText size="s" color="subdued">{data.noDriftReason}</EuiText>
          ) : (
            <EuiBasicTable<DriftFinding>
              items={data.driftFindings}
              columns={[
                { field: 'objectKind', name: 'Object' },
                { field: 'naturalKey', name: 'Key' },
                { field: 'change', name: 'Change' },
                { field: 'field', name: 'Field' },
              ]}
              noItemsMessage="No drift findings."
            />
          )}
        </EuiPanel>
      )}

      {confirmDelete && (
        <EuiConfirmModal
          title="Remove this node?"
          onCancel={() => setConfirmDelete(false)}
          onConfirm={() => del.mutate(nodeID, { onSuccess: () => navigate('/nodes') })}
          cancelButtonText="Cancel"
          confirmButtonText="Remove"
          buttonColor="danger"
          isLoading={del.isPending}
        >
          This removes the node and its collected data. This cannot be undone.
        </EuiConfirmModal>
      )}
    </>
  )
}
