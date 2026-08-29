import { EuiBasicTable, EuiButton, EuiFieldSearch, EuiFlexGroup, EuiFlexItem, EuiLoadingChart, EuiPanel, EuiSelect, EuiSpacer, EuiText, EuiTitle } from '@elastic/eui'
import { useSearchParams } from 'react-router-dom'
import { useAudit } from '../../api/queries/settings'
import type { AuditEventItem } from '../../api/pb/nagipath/api/v1/settings_pb'
import { SettingsSubNav } from './SettingsSubNav'

// Ported from internal/web/templates/audit.html against
// GET /api/ui/settings/audit — export stays a plain link (?export=csv),
// same as the pre-SPA page, since it is a browser download, not a fetch.
export function AuditPage() {
  const [params, setParams] = useSearchParams()
  const actor = params.get('actor') ?? ''
  const action = params.get('action') ?? ''
  const range = params.get('range') ?? '7d'
  const page = Number(params.get('page') ?? '1')
  const { data, isPending, isError, error } = useAudit({ actor, action, range, page })

  const columns = [
    { field: 'at', name: 'When', render: (v: string) => new Date(v).toLocaleString() },
    { field: 'actorLabel', name: 'Actor' },
    { field: 'action', name: 'Action' },
    { field: 'targetLabel', name: 'Target', render: (v: string, row: AuditEventItem) => v ? `${row.targetKind}: ${v}` : row.targetKind },
    { field: 'outcome', name: 'Outcome' },
    { field: 'sourceIp', name: 'Source', render: (v: string) => v || '—' },
    { field: 'detail', name: 'Detail', render: (v: string) => v || '—' },
  ]

  const exportURL = `/api/ui/settings/audit?export=csv${actor ? `&actor=${encodeURIComponent(actor)}` : ''}${action ? `&action=${encodeURIComponent(action)}` : ''}${range ? `&range=${range}` : ''}`

  return (
    <>
      <SettingsSubNav />
      <EuiTitle size="m"><h1>Audit log</h1></EuiTitle>
      <EuiSpacer />

      <EuiFlexGroup gutterSize="s">
        <EuiFlexItem grow={false} style={{ width: 220 }}>
          <EuiFieldSearch placeholder="actor" defaultValue={actor} onSearch={(v) => setParams((p) => { p.set('actor', v); p.set('page', '1'); return p })} />
        </EuiFlexItem>
        <EuiFlexItem grow={false}>
          <EuiSelect
            options={[{ value: '', text: 'Action: any' }, ...(data?.actions ?? []).map((a) => ({ value: a, text: a }))]}
            value={action}
            onChange={(e) => setParams((p) => { p.set('action', e.target.value); p.set('page', '1'); return p })}
          />
        </EuiFlexItem>
        <EuiFlexItem grow={false}>
          <EuiSelect
            options={[{ value: '24h', text: 'Range: 24 hours' }, { value: '7d', text: 'Range: 7 days' }, { value: '30d', text: 'Range: 30 days' }, { value: '90d', text: 'Range: 90 days' }]}
            value={range}
            onChange={(e) => setParams((p) => { p.set('range', e.target.value); p.set('page', '1'); return p })}
          />
        </EuiFlexItem>
        <EuiFlexItem grow={false}>
          <EuiButton href={exportURL} iconType="download">Export CSV</EuiButton>
        </EuiFlexItem>
      </EuiFlexGroup>
      <EuiSpacer />

      {isPending && <EuiLoadingChart size="xl" />}
      {isError && <EuiText color="danger">{error.message}</EuiText>}
      {data && (
        <EuiPanel>
          <EuiText size="s"><strong>{data.total}</strong> total event(s), page {data.page} of {Math.max(data.totalPages, 1)}</EuiText>
          <EuiSpacer size="s" />
          <EuiBasicTable<AuditEventItem> items={data.events} columns={columns} rowHeader="at" noItemsMessage="No matching audit events." />
          <EuiSpacer size="s" />
          <EuiFlexGroup gutterSize="s">
            <EuiFlexItem grow={false}>
              <EuiButton size="s" isDisabled={page <= 1} onClick={() => setParams((p) => { p.set('page', String(page - 1)); return p })}>Previous</EuiButton>
            </EuiFlexItem>
            <EuiFlexItem grow={false}>
              <EuiButton size="s" isDisabled={page >= data.totalPages} onClick={() => setParams((p) => { p.set('page', String(page + 1)); return p })}>Next</EuiButton>
            </EuiFlexItem>
          </EuiFlexGroup>
        </EuiPanel>
      )}
    </>
  )
}
