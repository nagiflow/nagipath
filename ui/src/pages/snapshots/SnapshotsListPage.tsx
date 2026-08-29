import {
  EuiBadge,
  EuiBasicTable,
  EuiButton,
  EuiFieldSearch,
  EuiFlexGroup,
  EuiFlexItem,
  EuiLoadingChart,
  EuiPageTemplate,
  EuiPanel,
  EuiSelect,
  EuiSpacer,
  EuiText,
  EuiTitle,
} from '@elastic/eui'
import { useSearchParams } from 'react-router-dom'
import { useSnapshots } from '../../api/queries/snapshots'
import type { SnapshotRow } from '../../api/pb/nagipath/api/v1/snapshots_pb'

const stateColor: Record<string, 'success' | 'warning' | 'danger'> = { ok: 'success', degraded: 'warning', failed: 'danger' }

// Ported from internal/web/templates/snapshots.html against GET
// /api/ui/snapshots (internal/api/snapshots.go) — same range/query filters,
// same cursor "Load more" pagination.
export function SnapshotsListPage() {
  const [params, setParams] = useSearchParams()
  const q = params.get('q') ?? ''
  const range = params.get('range') ?? '24h'
  const cursor = params.get('cursor') ?? ''
  const { data, isPending, isError, error } = useSnapshots({ q, range, cursor })

  if (isPending) return <EuiPageTemplate.EmptyPrompt icon={<EuiLoadingChart size="xl" />} title={<h2>Loading snapshots…</h2>} />
  if (isError) return <EuiPageTemplate.EmptyPrompt iconType="alert" color="danger" title={<h2>Could not load snapshots</h2>} body={<p>{error.message}</p>} />

  if (data.empty) {
    return <EuiPageTemplate.EmptyPrompt title={<h2>Nothing collected yet</h2>} body={<p>Collect from a node first.</p>} />
  }

  const columns = [
    { field: 'capturedAt', name: 'Captured', render: (v: string) => new Date(v).toLocaleString() },
    { field: 'instance', name: 'Instance' },
    { field: 'node', name: 'Node' },
    { field: 'cluster', name: 'Cluster' },
    { field: 'trigger', name: 'Trigger' },
    { field: 'changed', name: 'Changed', render: (c: boolean) => (c ? <EuiBadge color="primary">changed</EuiBadge> : null) },
    { field: 'state', name: 'State', render: (s: string) => <EuiBadge color={stateColor[s] ?? 'default'}>{s}</EuiBadge> },
  ]

  return (
    <>
      <EuiTitle size="m"><h1>Snapshots</h1></EuiTitle>
      <EuiSpacer size="s" />
      <EuiText size="s" color="subdued">
        {data.filtered} of {data.total} in window · {data.changed} changed · retention {data.retentionDays}d
      </EuiText>
      <EuiSpacer />

      <EuiFlexGroup gutterSize="s">
        <EuiFlexItem grow={false} style={{ width: 260 }}>
          <EuiFieldSearch
            placeholder="filter by instance, node, cluster"
            defaultValue={q}
            onSearch={(v) => setParams((p) => { if (v) p.set('q', v); else p.delete('q'); p.delete('cursor'); return p })}
          />
        </EuiFlexItem>
        <EuiFlexItem grow={false}>
          <EuiSelect
            options={[
              { value: '24h', text: 'Last 24h' },
              { value: '7d', text: 'Last 7 days' },
              { value: '30d', text: 'Last 30 days' },
              { value: '90d', text: 'Last 90 days' },
            ]}
            value={range}
            onChange={(e) => setParams((p) => { p.set('range', e.target.value); p.delete('cursor'); return p })}
          />
        </EuiFlexItem>
      </EuiFlexGroup>
      <EuiSpacer size="s" />

      <EuiPanel>
        <EuiBasicTable<SnapshotRow> items={data.list} columns={columns} rowHeader="instance" noItemsMessage="No snapshots in this window." />
        {data.hasMore && (
          <>
            <EuiSpacer size="s" />
            <EuiButton size="s" onClick={() => setParams((p) => { p.set('cursor', data.nextCursor); return p })}>
              Load more
            </EuiButton>
          </>
        )}
      </EuiPanel>
    </>
  )
}
