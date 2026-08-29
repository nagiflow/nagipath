import { useState } from 'react'
import { EuiBadge, EuiBasicTable, EuiButton, EuiCallOut, EuiFieldNumber, EuiFieldText, EuiForm, EuiFormRow, EuiLoadingChart, EuiPanel, EuiSpacer, EuiText, EuiTitle } from '@elastic/eui'
import { useSession } from '../../api/queries/session'
import { useAPIKeys, useCreateAPIKey, useRevokeAPIKey } from '../../api/queries/settings'
import type { ApiKeyItem } from '../../api/pb/nagipath/api/v1/settings_pb'
import { SettingsSubNav } from './SettingsSubNav'

// Ported from internal/web/templates/api_keys.html against
// GET/POST /api/ui/settings/api-keys and .../{id}/revoke. The raw token is
// shown exactly once, in the create response — this page never fetches it
// back, matching the old page's one-time-reveal behavior.
export function ApiKeysPage() {
  const { data, isPending, isError, error } = useAPIKeys('')
  const create = useCreateAPIKey()
  const revoke = useRevokeAPIKey()
  const { data: session } = useSession()
  const [form, setForm] = useState({ name: '', expiresDays: 365 })
  const [createdToken, setCreatedToken] = useState('')

  const columns = [
    { field: 'name', name: 'Name' },
    { field: 'prefix', name: 'Prefix' },
    { field: 'username', name: 'Created by' },
    { field: 'createdAt', name: 'Created', render: (v: string) => new Date(v).toLocaleString() },
    { field: 'expiresAt', name: 'Expires', render: (v: string) => (v ? new Date(v).toLocaleString() : 'never') },
    {
      field: 'revokedAt', name: 'Status',
      render: (v: string) => (v ? <EuiBadge color="default">revoked</EuiBadge> : <EuiBadge color="success">active</EuiBadge>),
    },
    {
      name: 'Actions',
      render: (row: ApiKeyItem) =>
        !row.revokedAt && session?.user?.role === 'admin' ? (
          <EuiButton size="s" color="danger" isLoading={revoke.isPending} onClick={() => revoke.mutate(row.id)}>Revoke</EuiButton>
        ) : null,
    },
  ]

  return (
    <>
      <SettingsSubNav />
      <EuiTitle size="m"><h1>API keys</h1></EuiTitle>
      <EuiSpacer />

      {createdToken && (
        <>
          <EuiCallOut title="Copy this token now — it will not be shown again" color="warning" iconType="alert">
            <code>{createdToken}</code>
          </EuiCallOut>
          <EuiSpacer />
        </>
      )}

      {isPending && <EuiLoadingChart size="xl" />}
      {isError && <EuiText color="danger">{error.message}</EuiText>}
      {data && (
        <EuiPanel>
          <EuiBasicTable<ApiKeyItem> items={data.keys} columns={columns} rowHeader="name" noItemsMessage="No API keys yet." />
        </EuiPanel>
      )}

      {session?.user?.role === 'admin' && (
        <>
          <EuiSpacer />
          <EuiPanel>
            <EuiTitle size="xs"><h2>Create an API key</h2></EuiTitle>
            <EuiSpacer size="s" />
            <EuiForm component="div">
              <EuiFormRow label="Name"><EuiFieldText value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} /></EuiFormRow>
              <EuiFormRow label="Expires in (days)">
                <EuiFieldNumber value={form.expiresDays} onChange={(e) => setForm({ ...form, expiresDays: Number(e.target.value) })} />
              </EuiFormRow>
              {create.isError && <EuiText color="danger" size="s">{create.error.message}</EuiText>}
              <EuiSpacer size="s" />
              <EuiButton isLoading={create.isPending} onClick={() => create.mutate(form, {
                onSuccess: (res) => {
                  setCreatedToken(typeof res.token === 'string' ? res.token : '')
                  setForm({ name: '', expiresDays: 365 })
                },
              })}>
                Create key
              </EuiButton>
            </EuiForm>
          </EuiPanel>
        </>
      )}
    </>
  )
}
