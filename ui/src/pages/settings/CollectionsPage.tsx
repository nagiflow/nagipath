import { EuiBadge, EuiBasicTable, EuiButton, EuiFieldSearch, EuiFlexGroup, EuiFlexItem, EuiLoadingChart, EuiPanel, EuiSelect, EuiSpacer, EuiStat, EuiText, EuiTitle } from '@elastic/eui'
import { useSearchParams } from 'react-router-dom'
import { useCollections } from '../../api/queries/settings'
import type { CollectionRow } from '../../api/pb/nagipath/api/v1/settings_pb'
import { SettingsSubNav } from './SettingsSubNav'

const stateColor: Record<string, 'success' | 'warning' | 'danger' | 'primary'> = {
  succeeded: 'success', degraded: 'warning', failed: 'danger', running: 'primary',
}

// Ported from internal/web/templates/collections.html against
// GET /api/ui/collections (internal/api/collections.go) — same filters,
// same "Load more" cursor pagination, same filtered CSV export link.
export function CollectionsPage() {
  const [params, setParams] = useSearchParams()
  const node = params.get('node') ?? ''
  const status = params.get('status') ?? ''
  const trigger = params.get('trigger') ?? ''
  const range = params.get('range') ?? '24h'
  const cursor = params.get('cursor') ?? ''
  const { data, isPending, isError, error } = useCollections({ node, status, trigger, range, cursor })

  const columns = [
    { field: 'startedAt', name: 'Started', render: (v: string) => new Date(v).toLocaleString() },
    { field: 'nodeName', name: 'Node' },
    { field: 'trigger', name: 'Trigger' },
    { field: 'durationMs', name: 'Duration', render: (v: bigint) => `${v} ms` },
    { field: 'outcome', name: 'Outcome' },
    { field: 'status', name: 'State', render: (v: string) => <EuiBadge color={stateColor[v] ?? 'default'}>{v}</EuiBadge> },
  ]

  const exportURL = `/api/ui/collections?export=csv&range=${range}${node ? `&node=${encodeURIComponent(node)}` : ''}${status ? `&status=${status}` : ''}${trigger ? `&trigger=${trigger}` : ''}`

  return (
    <>
      <SettingsSubNav />
      <EuiTitle size="m"><h1>Collection jobs</h1></EuiTitle>
      <EuiSpacer />

      {isPending && <EuiLoadingChart size="xl" />}
      {isError && <EuiText color="danger">{error.message}</EuiText>}
      {data?.empty && <EuiText color="subdued">No nodes yet — add one under Nodes first.</EuiText>}

      {data && !data.empty && (
        <>
          <EuiFlexGroup>
            <EuiFlexItem><EuiStat title={data.total} description="Runs" titleSize="s" /></EuiFlexItem>
            <EuiFlexItem><EuiStat title={data.succeeded} description="Succeeded" titleSize="s" /></EuiFlexItem>
            <EuiFlexItem><EuiStat title={data.degraded} description="Degraded" titleSize="s" /></EuiFlexItem>
            <EuiFlexItem><EuiStat title={data.failed} description="Failed" titleSize="s" /></EuiFlexItem>
            <EuiFlexItem><EuiStat title={`${data.medianMs} ms`} description="Median duration" titleSize="s" /></EuiFlexItem>
            <EuiFlexItem><EuiStat title={data.running} description="In queue" titleSize="s" /></EuiFlexItem>
          </EuiFlexGroup>
          <EuiSpacer />

          <EuiFlexGroup gutterSize="s">
            <EuiFlexItem grow={false} style={{ width: 220 }}>
              <EuiFieldSearch placeholder="node" defaultValue={node} onSearch={(v) => setParams((p) => { p.set('node', v); p.delete('cursor'); return p })} />
            </EuiFlexItem>
            <EuiFlexItem grow={false}>
              <EuiSelect
                options={[{ value: '', text: 'Status: any' }, { value: 'succeeded', text: 'Succeeded' }, { value: 'degraded', text: 'Degraded' }, { value: 'failed', text: 'Failed' }, { value: 'running', text: 'Running' }]}
                value={status} onChange={(e) => setParams((p) => { p.set('status', e.target.value); p.delete('cursor'); return p })}
              />
            </EuiFlexItem>
            <EuiFlexItem grow={false}>
              <EuiSelect
                options={[{ value: '', text: 'Trigger: any' }, { value: 'manual', text: 'Manual' }, { value: 'scheduled', text: 'Scheduled' }]}
                value={trigger} onChange={(e) => setParams((p) => { p.set('trigger', e.target.value); p.delete('cursor'); return p })}
              />
            </EuiFlexItem>
            <EuiFlexItem grow={false}>
              <EuiSelect
                options={[{ value: '24h', text: 'Range: 24 hours' }, { value: '7d', text: 'Range: 7 days' }, { value: '30d', text: 'Range: 30 days' }, { value: '90d', text: 'Range: 90 days' }]}
                value={range} onChange={(e) => setParams((p) => { p.set('range', e.target.value); p.delete('cursor'); return p })}
              />
            </EuiFlexItem>
            <EuiFlexItem grow={false}>
              <EuiButton href={exportURL} iconType="download">Export CSV</EuiButton>
            </EuiFlexItem>
          </EuiFlexGroup>
          <EuiSpacer />

          <EuiPanel>
            <EuiText size="s" color="subdued">
              {data.from}–{data.to} of {data.total}
            </EuiText>
            <EuiSpacer size="s" />
            <EuiBasicTable<CollectionRow> items={data.rows} columns={columns} rowHeader="startedAt" noItemsMessage="No collections match these filters." />
            {data.hasMore && (
              <>
                <EuiSpacer size="s" />
                <EuiButton size="s" onClick={() => setParams((p) => { p.set('cursor', data.nextCursor); return p })}>Load more</EuiButton>
              </>
            )}
          </EuiPanel>
        </>
      )}
    </>
  )
}
