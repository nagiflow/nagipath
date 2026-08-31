import { useState } from 'react'
import type { Meta, StoryObj } from '@storybook/react-vite'
import { Badge, Disclosure, Kv, Panel, PanelHeader, Table, type Column } from '.'

interface Row { id: number; node: string; cluster: string; listeners: string; state: string }

const rows: Row[] = [
  { id: 12, node: 'app-iad3-01', cluster: 'app-iad3', listeners: '443 ssl, 80', state: 'conforming' },
  { id: 41, node: 'app-iad3-17', cluster: 'app-iad3', listeners: '443 ssl, 80', state: 'drift' },
  { id: 88, node: 'app-sfo2-04', cluster: 'app-sfo2', listeners: '443 ssl', state: 'conforming' },
  { id: 104, node: 'lb-legacy-04', cluster: 'legacy', listeners: '8443 ssl', state: 'failed' },
]

const columns: Column<Row>[] = [
  { name: 'Node', render: (r) => <span className="m">{r.node}</span> },
  { name: 'Cluster', width: 120, render: (r) => <span className="m mu">{r.cluster}</span> },
  { name: 'Listeners', render: (r) => <span className="m mu">{r.listeners}</span> },
  { name: 'State', width: 120, render: (r) => <Badge state={r.state} /> },
]

const meta: Meta<typeof Table<Row>> = {
  title: 'Components/Table',
  component: Table,
  parameters: {
    docs: {
      description: {
        component:
          "design/'s `.t` inside a `.pnl.z`: sticky header, zebra rows, fixed layout, cells that ellipsis rather than wrap. A row's detail expands **under the row** — the wireframe has no right-hand preview pane anywhere, because reading one means re-anchoring on which row you were on.",
      },
    },
  },
}
export default meta

type Story = StoryObj<typeof meta>

export const Default: Story = { args: { columns, items: rows, rowKey: (r: Row) => String(r.id) } }

export const Empty: Story = {
  args: { columns, items: [], rowKey: (r: Row) => String(r.id), emptyMessage: 'No node matches this filter.' },
}

// The full list-screen shape every inventory page uses: panel header, table,
// footer with the row count. No `pagination` prop here — a page whose
// endpoint returns everything at once (like Nodes) has nothing to paginate.
export const InPanel: Story = {
  render: () => (
    <div style={{ height: 320, display: 'flex' }}>
      <Panel z style={{ flex: 1 }}>
        <PanelHeader title="Nodes" meta="428 total" />
        <Table columns={columns} items={rows} rowKey={(r) => String(r.id)} />
      </Panel>
    </div>
  ),
}

// Cursor pagination (`pagination.kind: 'cursor'`) — every list whose endpoint
// pages by cursor (docs/frontend/not_built.md: "no page 3 to jump to").
// "Load more" only shows while `hasMore` is true; click it to see it fetch
// the last page and disappear, same as a real "cursor: null" response.
function CursorPaginated() {
  const [loaded, setLoaded] = useState(false)
  return (
    <Panel z style={{ height: 320 }}>
      <PanelHeader title="Probes" meta="one request each" />
      <Table
        columns={columns}
        items={rows}
        rowKey={(r) => String(r.id)}
        pagination={{
          kind: 'cursor',
          from: 1,
          to: loaded ? 46 : 4,
          total: 46,
          hasMore: !loaded,
          onLoadMore: () => setLoaded(true),
        }}
      />
    </Panel>
  )
}

export const CursorPagination: Story = { render: () => <CursorPaginated /> }

// Numbered pagination (`pagination.kind: 'pages'`) — only where the endpoint
// itself counts pages (the audit log). The strip collapses to
// first/last/current±1 with an ellipsis once there are enough pages.
function PagesPaginated() {
  const [page, setPage] = useState(4)
  return (
    <Panel z style={{ height: 320 }}>
      <PanelHeader title="Audit events" meta="12,406 in filter" />
      <Table
        columns={columns}
        items={rows}
        rowKey={(r) => String(r.id)}
        pagination={{
          kind: 'pages',
          page,
          totalPages: 9,
          onGoto: setPage,
          from: (page - 1) * 50 + 1,
          to: page * 50,
          total: 421,
        }}
      />
    </Panel>
  )
}

export const PagesPagination: Story = { render: () => <PagesPaginated /> }

function ExpandableTable() {
  const [open, setOpen] = useState<number | null>(41)
  const withDisclosure: Column<Row>[] = [
    { name: 'Node', render: (r) => <span className="m"><Disclosure open={open === r.id} />{r.node}</span> },
    ...columns.slice(1),
  ]
  return (
    <Panel z>
      <Table
        columns={withDisclosure}
        items={rows}
        rowKey={(r) => String(r.id)}
        onRowClick={(r) => setOpen(open === r.id ? null : r.id)}
        rowClassName={(r) => (open === r.id ? 'hl' : undefined)}
        renderExpanded={(r) =>
          open === r.id ? (
            <Kv
              rows={[
                ['address', '10.4.12.17:22'],
                ['vendor', 'nginx 1.24.0'],
                ['last run', 'degraded · 2 includes unreadable'],
                ['state', r.state],
              ]}
            />
          ) : null
        }
      />
    </Panel>
  )
}

export const ExpandedRow: Story = { render: () => <ExpandableTable /> }
