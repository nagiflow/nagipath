import { useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useSnapshots } from '../../api/queries/snapshots'
import type { SnapshotRow } from '../../api/pb/nagipath/api/v1/snapshots_pb'
import { PageHeader } from '../../components/shared/PageHeader'
import { StateBadge } from '../../components/shared/StateBadge'
import { Badge, Button, EmptyPrompt, Field, Loading, Panel, Select, Table, type Column } from '../../components/ui'

// Ported from internal/web/templates/snapshots.html against GET
// /api/snapshots (internal/api/snapshots.go), laid out as design/'s screen 2r —
// same range/query filters, same cursor "Load more" pagination. No sidebar row:
// Snapshots is reached from a node or a provenance link, so breadcrumbFor names
// the Inventory section and no nav item is active.
export function SnapshotsListPage() {
  const [params, setParams] = useSearchParams()
  const q = params.get('q') ?? ''
  const range = params.get('range') ?? '24h'
  const cursor = params.get('cursor') ?? ''
  const [search, setSearch] = useState(q)
  const { data, isPending, isError, error } = useSnapshots({ q, range, cursor })

  if (isPending) return <Loading label="Loading snapshots…" />
  if (isError) return <EmptyPrompt danger title="Could not load snapshots" body={error.message} />

  if (data.empty) {
    return <EmptyPrompt title="Nothing collected yet" body="Collect from a node first." />
  }

  const commit = () => setParams((p) => { if (search) p.set('q', search); else p.delete('q'); p.delete('cursor'); return p })

  const columns: Column<SnapshotRow>[] = [
    { name: 'Captured', render: (r) => new Date(r.capturedAt).toLocaleString() },
    { name: 'Instance', render: (r) => r.instance },
    { name: 'Node', render: (r) => r.node },
    { name: 'Cluster', render: (r) => r.cluster },
    { name: 'Trigger', render: (r) => r.trigger },
    { name: 'Changed', render: (r) => (r.changed ? <Badge cls="n">changed</Badge> : null) },
    { name: 'State', render: (r) => <StateBadge state={r.state} /> },
  ]

  return (
    <>
      <PageHeader
        title="Snapshots"
        meta={`${data.filtered} of ${data.total} in window · ${data.changed} changed · retention ${data.retentionDays} d`}
      />
      <div className="bd">
        <div className="row" style={{ flex: 'none' }}>
          <div style={{ width: 260 }}>
            <Field placeholder="filter by instance, node, cluster" value={search} onChange={setSearch} onEnter={commit} grow />
          </div>
          <Select
            options={[
              { value: '24h', text: 'Last 24h' },
              { value: '7d', text: 'Last 7 days' },
              { value: '30d', text: 'Last 30 days' },
              { value: '90d', text: 'Last 90 days' },
            ]}
            value={range}
            onChange={(v) => setParams((p) => { p.set('range', v); p.delete('cursor'); return p })}
          />
          <span className="m mus">reached from a node, not from the sidebar</span>
        </div>

        <Panel z>
          <Table<SnapshotRow>
            items={data.list}
            columns={columns}
            rowKey={(r) => r.id.toString()}
            emptyMessage="No snapshots in this window."
          />
          {data.hasMore && (
            <div style={{ padding: '8px 12px' }}>
              <Button small onClick={() => setParams((p) => { p.set('cursor', data.nextCursor); return p })}>
                Load more
              </Button>
            </div>
          )}
        </Panel>
      </div>
    </>
  )
}
