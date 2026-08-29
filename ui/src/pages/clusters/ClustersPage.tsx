import { useState } from 'react'
import {
  EuiBadge,
  EuiBasicTable,
  EuiButton,
  EuiFieldSearch,
  EuiFieldText,
  EuiFlexGroup,
  EuiFlexItem,
  EuiFormRow,
  EuiLink,
  EuiLoadingChart,
  EuiPageTemplate,
  EuiPanel,
  EuiSelect,
  EuiSpacer,
  EuiText,
  EuiTitle,
} from '@elastic/eui'
import { useSearchParams } from 'react-router-dom'
import { useClusters, useRenameCluster } from '../../api/queries/clusters'
import { useSession } from '../../api/queries/session'
import type { ClusterListItem } from '../../api/types'

// Ported from internal/web/templates/clusters.html against
// GET/POST /api/ui/clusters (internal/api/clusters.go) — same filters, same
// sort orders, same master/detail layout. "Change baseline" still posts to
// the old server-rendered /drift/golden (Drift isn't ported until Phase 3),
// as a plain form so the CSRF flow matches what that endpoint expects today.
export function ClustersPage() {
  const [params, setParams] = useSearchParams()
  const { data: session } = useSession()
  const q = params.get('q') ?? ''
  const drift = params.get('drift') ?? 'any'
  const sortBy = params.get('sort') ?? 'drift'
  const selectedID = params.get('cluster') ? Number(params.get('cluster')) : undefined

  const { data, isPending, isError, error } = useClusters({ q, drift, sort: sortBy, cluster: selectedID })
  const rename = useRenameCluster()
  const [renameValue, setRenameValue] = useState('')

  if (isPending) return <EuiPageTemplate.EmptyPrompt icon={<EuiLoadingChart size="xl" />} title={<h2>Loading clusters…</h2>} />
  if (isError) return <EuiPageTemplate.EmptyPrompt iconType="alert" color="danger" title={<h2>Could not load clusters</h2>} body={<p>{error.message}</p>} />

  const columns = [
    {
      field: 'name',
      name: 'Cluster',
      render: (name: string, row: ClusterListItem) => (
        <EuiLink onClick={() => setParams((p) => { p.set('cluster', String(row.id)); return p })}>{name}</EuiLink>
      ),
    },
    { field: 'members', name: 'Proc.' },
    { field: 'vendor', name: 'Vendor' },
    { field: 'golden_peer_name', name: 'Baseline', render: (v?: string) => v || <EuiText color="subdued" size="s">not set</EuiText> },
    {
      field: 'drift_count',
      name: 'Drift',
      render: (v: number) => (v > 0 ? <EuiBadge color="warning">{v}</EuiBadge> : <EuiText color="subdued" size="s">—</EuiText>),
    },
    { field: 'certs_expiring_30d', name: 'Certs ≤30d', render: (v: number) => (v > 0 ? v : <EuiText color="subdued" size="s">—</EuiText>) },
    {
      field: 'last_collected',
      name: 'Last collected',
      render: (v: string | undefined, row: ClusterListItem) =>
        v ? `${new Date(v).toLocaleString()} · ${row.instances_collected}/${row.members}` : <EuiText color="subdued" size="s">never</EuiText>,
    },
  ]

  return (
    <>
      <EuiFlexGroup alignItems="center" justifyContent="spaceBetween">
        <EuiFlexItem grow={false}>
          <EuiTitle size="m">
            <h1>Clusters</h1>
          </EuiTitle>
          <EuiText color="subdued" size="s">
            discovered from configuration · rename only
          </EuiText>
        </EuiFlexItem>
      </EuiFlexGroup>
      <EuiSpacer />

      <EuiFlexGroup gutterSize="s">
        <EuiFlexItem grow={false} style={{ width: 260 }}>
          <EuiFieldSearch
            placeholder="filter clusters"
            defaultValue={q}
            onSearch={(v) => setParams((p) => { p.set('q', v); return p })}
          />
        </EuiFlexItem>
        <EuiFlexItem grow={false}>
          <EuiSelect
            options={[
              { value: 'any', text: 'Drift: any' },
              { value: 'drifted', text: 'Drift: drifted only' },
              { value: 'clean', text: 'Drift: clean' },
            ]}
            value={drift}
            onChange={(e) => setParams((p) => { p.set('drift', e.target.value); return p })}
          />
        </EuiFlexItem>
        <EuiFlexItem grow={false}>
          <EuiSelect
            options={[
              { value: 'drift', text: 'Sort: drift desc' },
              { value: 'name', text: 'Sort: name' },
              { value: 'certs', text: 'Sort: certs desc' },
              { value: 'collected', text: 'Sort: last collected' },
            ]}
            value={sortBy}
            onChange={(e) => setParams((p) => { p.set('sort', e.target.value); return p })}
          />
        </EuiFlexItem>
      </EuiFlexGroup>
      <EuiSpacer />

      <EuiFlexGroup>
        <EuiFlexItem grow={3}>
          <EuiPanel>
            <EuiText size="s">
              <strong>{data.clusters.length}</strong> cluster{data.clusters.length === 1 ? '' : 's'}
            </EuiText>
            <EuiSpacer size="s" />
            <EuiBasicTable<ClusterListItem>
              items={data.clusters}
              columns={columns}
              rowHeader="name"
              noItemsMessage="No clusters discovered. A cluster appears when two instances of the same vendor hold byte-identical configuration."
            />
          </EuiPanel>
        </EuiFlexItem>

        <EuiFlexItem grow={1}>
          <EuiPanel>
            {!data.selected ? (
              <>
                <EuiTitle size="xs">
                  <h2>Pick a cluster</h2>
                </EuiTitle>
                <EuiText size="s" color="subdued">
                  Select a cluster to see its members and divergence details.
                </EuiText>
              </>
            ) : (
              <>
                <EuiTitle size="xs">
                  <h2>{data.selected.name}</h2>
                </EuiTitle>
                <EuiSpacer size="s" />
                <EuiText size="s">
                  <p>nodes: {data.selected.members}</p>
                  <p>baseline: {data.selected.golden_peer_name || 'not set'}</p>
                  {data.selected.vendor && <p>vendor: {data.selected.vendor}</p>}
                  <p>last collected: {data.selected.last_collected ? new Date(data.selected.last_collected).toLocaleString() : 'never'}</p>
                </EuiText>

                {session?.user.role === 'admin' && (
                  <>
                    <EuiSpacer size="s" />
                    <form method="post" action="/drift/golden">
                      <input type="hidden" name="csrf" value={session.csrf_token} />
                      <input type="hidden" name="cluster" value={data.selected.id} />
                      <EuiButton type="submit" size="s">Change baseline</EuiButton>
                    </form>
                    <EuiSpacer size="s" />
                    <EuiFormRow label="Rename">
                      <EuiFlexGroup gutterSize="xs">
                        <EuiFlexItem>
                          <EuiFieldText
                            compressed
                            value={renameValue || data.selected.name}
                            onChange={(e) => setRenameValue(e.target.value)}
                          />
                        </EuiFlexItem>
                        <EuiFlexItem grow={false}>
                          <EuiButton
                            size="s"
                            isLoading={rename.isPending}
                            onClick={() => rename.mutate({ cluster: data.selected!.id, name: renameValue || data.selected!.name })}
                          >
                            Rename
                          </EuiButton>
                        </EuiFlexItem>
                      </EuiFlexGroup>
                    </EuiFormRow>
                  </>
                )}

                <EuiSpacer size="m" />
                <EuiTitle size="xxs">
                  <h3>Members ({data.selected.member_list.length})</h3>
                </EuiTitle>
                <EuiSpacer size="xs" />
                {data.selected.member_list.length === 0 ? (
                  <EuiText size="s" color="subdued">No members in this cluster.</EuiText>
                ) : (
                  data.selected.member_list.map((m) => (
                    <EuiFlexGroup key={m.id} gutterSize="s" alignItems="center" style={{ padding: '4px 0' }}>
                      <EuiFlexItem>
                        <EuiLink href={`/instances/${m.id}`}>{m.display_name}</EuiLink>
                      </EuiFlexItem>
                      <EuiFlexItem grow={false}>
                        <EuiText size="xs" color="subdued">
                          {m.is_golden ? 'GOLDEN' : m.divergence === undefined ? 'not compared' : m.divergence === 0 ? 'clean' : `${m.divergence} diffs`}
                        </EuiText>
                      </EuiFlexItem>
                    </EuiFlexGroup>
                  ))
                )}
                <EuiSpacer size="s" />
                <EuiButton size="s" href={`/drift?cluster=${data.selected.id}`}>
                  Open drift report
                </EuiButton>
              </>
            )}
          </EuiPanel>
        </EuiFlexItem>
      </EuiFlexGroup>
    </>
  )
}
