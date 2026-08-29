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
import { useClusters, useRenameCluster, useSetGoldenPeer } from '../../api/queries/clusters'
import { useSession } from '../../api/queries/session'
import type { ClusterListItem } from '../../api/pb/nagipath/api/v1/clusters_pb'

// Ported from internal/web/templates/clusters.html against
// GET/POST /api/ui/clusters (internal/api/clusters.go) — same filters, same
// sort orders, same master/detail layout. "Clear baseline" calls
// POST /api/ui/drift/golden (internal/api/drift.go) — the original form only
// ever cleared the golden peer (it had no instance picker), so the label was
// renamed to say what it actually does.
export function ClustersPage() {
  const [params, setParams] = useSearchParams()
  const { data: session } = useSession()
  const q = params.get('q') ?? ''
  const drift = params.get('drift') ?? 'any'
  const sortBy = params.get('sort') ?? 'drift'
  const selectedID = params.get('cluster') ? Number(params.get('cluster')) : undefined

  const { data, isPending, isError, error } = useClusters({ q, drift, sort: sortBy, cluster: selectedID })
  const rename = useRenameCluster()
  const golden = useSetGoldenPeer()
  const [renameValue, setRenameValue] = useState('')

  if (isPending) return <EuiPageTemplate.EmptyPrompt icon={<EuiLoadingChart size="xl" />} title={<h2>Loading clusters…</h2>} />
  if (isError) return <EuiPageTemplate.EmptyPrompt iconType="alert" color="danger" title={<h2>Could not load clusters</h2>} body={<p>{error.message}</p>} />

  const columns = [
    {
      field: 'name',
      name: 'Cluster',
      render: (name: string, row: ClusterListItem) => (
        <EuiLink onClick={() => setParams((p) => { p.set('cluster', row.id.toString()); return p })}>{name}</EuiLink>
      ),
    },
    { field: 'members', name: 'Proc.' },
    { field: 'vendor', name: 'Vendor' },
    { field: 'goldenPeerName', name: 'Baseline', render: (v?: string) => v || <EuiText color="subdued" size="s">not set</EuiText> },
    {
      field: 'driftCount',
      name: 'Drift',
      render: (v: number) => (v > 0 ? <EuiBadge color="warning">{v}</EuiBadge> : <EuiText color="subdued" size="s">—</EuiText>),
    },
    { field: 'certsExpiring30d', name: 'Certs ≤30d', render: (v: number) => (v > 0 ? v : <EuiText color="subdued" size="s">—</EuiText>) },
    {
      field: 'lastCollected',
      name: 'Last collected',
      render: (v: string | undefined, row: ClusterListItem) =>
        v ? `${new Date(v).toLocaleString()} · ${row.instancesCollected}/${row.members}` : <EuiText color="subdued" size="s">never</EuiText>,
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
                  <p>baseline: {data.selected.goldenPeerName || 'not set'}</p>
                  {data.selected.vendor && <p>vendor: {data.selected.vendor}</p>}
                  <p>last collected: {data.selected.lastCollected ? new Date(data.selected.lastCollected).toLocaleString() : 'never'}</p>
                </EuiText>

                {session?.user?.role === 'admin' && (
                  <>
                    <EuiSpacer size="s" />
                    <EuiButton
                      size="s"
                      isLoading={golden.isPending}
                      onClick={() => golden.mutate({ cluster: Number(data.selected!.id) })}
                    >
                      Clear baseline
                    </EuiButton>
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
                            onClick={() => rename.mutate({ cluster: Number(data.selected!.id), name: renameValue || data.selected!.name })}
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
                  <h3>Members ({data.selected.memberList.length})</h3>
                </EuiTitle>
                <EuiSpacer size="xs" />
                {data.selected.memberList.length === 0 ? (
                  <EuiText size="s" color="subdued">No members in this cluster.</EuiText>
                ) : (
                  data.selected.memberList.map((m) => (
                    <EuiFlexGroup key={m.id.toString()} gutterSize="s" alignItems="center" style={{ padding: '4px 0' }}>
                      <EuiFlexItem>
                        <EuiLink href={`/instances/${m.id}`}>{m.displayName}</EuiLink>
                      </EuiFlexItem>
                      <EuiFlexItem grow={false}>
                        <EuiText size="xs" color="subdued">
                          {m.isGolden ? 'GOLDEN' : m.divergence === undefined ? 'not compared' : m.divergence === 0n ? 'clean' : `${m.divergence} diffs`}
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
