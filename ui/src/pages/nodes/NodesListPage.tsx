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
  EuiModal,
  EuiModalBody,
  EuiModalFooter,
  EuiModalHeader,
  EuiModalHeaderTitle,
  EuiPageTemplate,
  EuiPanel,
  EuiSpacer,
  EuiText,
  EuiTitle,
} from '@elastic/eui'
import { useSearchParams } from 'react-router-dom'
import { useAddNode, useNodes } from '../../api/queries/nodes'
import { useSession } from '../../api/queries/session'
import type { NodeListRow } from '../../api/pb/nagipath/api/v1/nodes_pb'

// Ported from internal/web/templates/nodes.html against GET/POST
// /api/ui/nodes (internal/api/nodes.go). The htmx row-expand-to-see-processes
// interaction isn't reproduced — the node detail page is one click away.
export function NodesListPage() {
  const [params, setParams] = useSearchParams()
  const q = params.get('q') ?? ''
  const { data, isPending, isError, error } = useNodes(q)
  const { data: session } = useSession()
  const [showAdd, setShowAdd] = useState(false)

  if (isPending) return <EuiPageTemplate.EmptyPrompt icon={<EuiLoadingChart size="xl" />} title={<h2>Loading nodes…</h2>} />
  if (isError) return <EuiPageTemplate.EmptyPrompt iconType="alert" color="danger" title={<h2>Could not load nodes</h2>} body={<p>{error.message}</p>} />

  const columns = [
    {
      field: 'displayName',
      name: 'Node',
      render: (name: string, row: NodeListRow) => <EuiLink href={`/nodes/${row.id}`}>{name}</EuiLink>,
    },
    { field: 'address', name: 'Address' },
    { field: 'vendor', name: 'Vendor' },
    { field: 'cluster', name: 'Cluster' },
    {
      field: 'consecutiveFailures',
      name: 'State',
      render: (n: number) => {
        if (n === 0) return <EuiBadge color="success">OK</EuiBadge>
        const quarantined = n >= (data.threshold || 10)
        return <EuiBadge color={quarantined ? 'danger' : 'warning'}>{quarantined ? 'QUARANTINED' : 'DEGRADED'}</EuiBadge>
      },
    },
    { field: 'pendingHostKeys', name: 'Host keys', render: (n: number) => (n > 0 ? <EuiBadge color="warning">{n} pending</EuiBadge> : null) },
    { field: 'lastCollection', name: 'Last collected', render: (v: string) => (v ? new Date(v).toLocaleString() : 'never') },
  ]

  return (
    <>
      <EuiFlexGroup alignItems="center" justifyContent="spaceBetween">
        <EuiFlexItem grow={false}>
          <EuiTitle size="m">
            <h1>Nodes</h1>
          </EuiTitle>
        </EuiFlexItem>
        {session?.user?.role === 'admin' && (
          <EuiFlexItem grow={false}>
            <EuiButton size="s" onClick={() => setShowAdd(true)}>Add node</EuiButton>
          </EuiFlexItem>
        )}
      </EuiFlexGroup>
      <EuiSpacer />

      {data.pending > 0 && (
        <>
          <EuiText size="s">
            <EuiLink href="/settings/hostkeys">{data.pending} host key(s) waiting for approval →</EuiLink>
          </EuiText>
          <EuiSpacer size="s" />
        </>
      )}

      <EuiFieldSearch
        placeholder="filter by name or address"
        defaultValue={q}
        onSearch={(v) => setParams((p) => { if (v) p.set('q', v); else p.delete('q'); return p })}
        style={{ maxWidth: 360 }}
      />
      <EuiSpacer size="s" />

      <EuiPanel>
        <EuiBasicTable<NodeListRow>
          items={data.nodes}
          columns={columns}
          rowHeader="displayName"
          noItemsMessage={q ? 'No nodes match this filter.' : 'No nodes added yet.'}
        />
      </EuiPanel>

      {showAdd && <AddNodeModal onClose={() => setShowAdd(false)} />}
    </>
  )
}

function AddNodeModal({ onClose }: { onClose: () => void }) {
  const [address, setAddress] = useState('')
  const [displayName, setDisplayName] = useState('')
  const add = useAddNode()

  const submit = () => {
    add.mutate(
      { address, display_name: displayName || undefined },
      { onSuccess: (res) => { window.location.assign(`/nodes/${res.id}`); onClose() } },
    )
  }

  return (
    <EuiModal onClose={onClose}>
      <EuiModalHeader>
        <EuiModalHeaderTitle>Add node</EuiModalHeaderTitle>
      </EuiModalHeader>
      <EuiModalBody>
        <EuiFormRow label="Address" helpText="One host at a time — nagipath does not scan networks.">
          <EuiFieldText value={address} onChange={(e) => setAddress(e.target.value)} placeholder="10.0.0.1" />
        </EuiFormRow>
        <EuiFormRow label="Display name (optional)">
          <EuiFieldText value={displayName} onChange={(e) => setDisplayName(e.target.value)} />
        </EuiFormRow>
        {add.isError && (
          <EuiText color="danger" size="s">{(add.error as Error).message}</EuiText>
        )}
      </EuiModalBody>
      <EuiModalFooter>
        <EuiButton onClick={onClose}>Cancel</EuiButton>
        <EuiButton fill isLoading={add.isPending} onClick={submit} disabled={!address}>
          Add
        </EuiButton>
      </EuiModalFooter>
    </EuiModal>
  )
}
