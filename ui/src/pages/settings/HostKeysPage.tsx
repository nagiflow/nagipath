import {
  EuiBadge,
  EuiBasicTable,
  EuiButton,
  EuiFlexGroup,
  EuiFlexItem,
  EuiLoadingChart,
  EuiPanel,
  EuiSelect,
  EuiSpacer,
  EuiText,
  EuiTitle,
} from '@elastic/eui'
import { useSearchParams } from 'react-router-dom'
import { useSession } from '../../api/queries/session'
import { useDecideHostKey, useHostKeys } from '../../api/queries/settings'
import type { PendingHostKey } from '../../api/pb/nagipath/api/v1/nodes_pb'
import { SettingsSubNav } from './SettingsSubNav'

// Ported from internal/web/templates/hostkeys.html against
// GET /api/ui/settings/hostkeys (internal/api/settings.go) — individual
// decisions post to POST /api/ui/hostkeys/{id}/decide (shipped in Phase 2).
// The bulk "approve all matching filter" action stays a legacy full-page
// POST to /hostkeys/approve (internal/web/inventory_import.go): that handler
// is outside this migration's scope, so the form below posts to it directly,
// CSRF field and all, exactly like a pre-SPA page would.
export function HostKeysPage() {
  const { data: session } = useSession()
  const [params, setParams] = useSearchParams()
  const state = params.get('state') ?? ''
  const cluster = params.get('cluster') ?? ''
  const { data, isPending, isError, error } = useHostKeys(state, cluster)
  const decide = useDecideHostKey()

  const columns = [
    { field: 'key.algorithm', name: 'Algorithm' },
    { field: 'nodeName', name: 'Node' },
    { field: 'key.fingerprint', name: 'Fingerprint' },
    { field: 'key.state', name: 'State', render: (v: string) => <EuiBadge color={v === 'approved' ? 'success' : v === 'pending' ? 'warning' : 'default'}>{v}</EuiBadge> },
    { field: 'previous', name: 'Previous', render: (v: string) => v || '—' },
    {
      name: 'Decide',
      render: (row: PendingHostKey) =>
        row.key?.state === 'pending' && session?.user?.role === 'admin' ? (
          <EuiFlexGroup gutterSize="xs">
            <EuiFlexItem grow={false}>
              <EuiButton size="s" onClick={() => decide.mutate({ id: Number(row.key!.id), approve: true })}>Approve</EuiButton>
            </EuiFlexItem>
            <EuiFlexItem grow={false}>
              <EuiButton size="s" color="danger" onClick={() => decide.mutate({ id: Number(row.key!.id), approve: false })}>Reject</EuiButton>
            </EuiFlexItem>
          </EuiFlexGroup>
        ) : null,
    },
  ]

  return (
    <>
      <SettingsSubNav />
      <EuiTitle size="m"><h1>Host keys</h1></EuiTitle>
      <EuiSpacer />

      {data?.stats && (
        <EuiText size="s" color="subdued">
          {data.stats.pending} pending · {data.stats.changed} changed · {data.stats.approved} approved
        </EuiText>
      )}
      <EuiSpacer size="s" />
      <EuiSelect
        options={[{ value: '', text: 'State: any' }, { value: 'pending', text: 'Pending' }, { value: 'approved', text: 'Approved' }, { value: 'changed', text: 'Changed' }]}
        value={state}
        onChange={(e) => setParams((p) => { p.set('state', e.target.value); return p })}
      />
      <EuiSpacer />

      {isPending && <EuiLoadingChart size="xl" />}
      {isError && <EuiText color="danger">{error.message}</EuiText>}
      {data && (
        <EuiPanel>
          <EuiBasicTable<PendingHostKey> items={data.keys} columns={columns} rowHeader="key.fingerprint"
            noItemsMessage="No host keys recorded yet." />
        </EuiPanel>
      )}

      {session?.user?.role === 'admin' && data && data.keys.some((k) => k.key?.state === 'pending') && (
        <>
          <EuiSpacer />
          <form method="post" action="/hostkeys/approve">
            <input type="hidden" name="csrf_token" value={session?.csrfToken ?? ''} />
            <input type="hidden" name="back" value={`/settings/hostkeys${state ? `?state=${state}` : ''}`} />
            {data.keys.filter((k) => k.key?.state === 'pending').map((k) => (
              <input key={k.key!.id.toString()} type="hidden" name="key" value={k.key!.id.toString()} />
            ))}
            <EuiButton type="submit">Approve all matching the current filter</EuiButton>
          </form>
        </>
      )}
    </>
  )
}
