import { useState } from 'react'
import {
  EuiBadge,
  EuiBasicTable,
  EuiButton,
  EuiFieldText,
  EuiForm,
  EuiFormRow,
  EuiLoadingChart,
  EuiPanel,
  EuiSelect,
  EuiSpacer,
  EuiSwitch,
  EuiText,
  EuiTitle,
} from '@elastic/eui'
import { useSession } from '../../api/queries/session'
import { useAddUser, useSetUserDisabled, useUsers } from '../../api/queries/settings'
import type { UserItem } from '../../api/pb/nagipath/api/v1/settings_pb'
import { SettingsSubNav } from './SettingsSubNav'

// Ported from internal/web/templates/users.html against
// GET/POST /api/ui/settings/users and .../{id}/disable|enable.
export function UsersPage() {
  const { data: session } = useSession()
  const { data, isPending, isError, error } = useUsers()
  const add = useAddUser()
  const setDisabled = useSetUserDisabled()
  const [form, setForm] = useState({ username: '', password: '', confirm: '', role: 'viewer', mustChange: false })

  const columns = [
    { field: 'username', name: 'Username' },
    { field: 'role', name: 'Role' },
    { field: 'lastLoginAt', name: 'Last login', render: (v: string) => (v ? new Date(v).toLocaleString() : 'never') },
    { field: 'disabled', name: 'Status', render: (v: boolean) => (v ? <EuiBadge color="default">disabled</EuiBadge> : <EuiBadge color="success">enabled</EuiBadge>) },
    {
      name: 'Actions',
      render: (row: UserItem) =>
        session?.user?.role === 'admin' ? (
          <EuiButton size="s" color={row.disabled ? 'primary' : 'danger'} isLoading={setDisabled.isPending}
            onClick={() => setDisabled.mutate({ id: row.id, disabled: !row.disabled })}>
            {row.disabled ? 'Enable' : 'Disable'}
          </EuiButton>
        ) : null,
    },
  ]

  return (
    <>
      <SettingsSubNav />
      <EuiTitle size="m"><h1>Users &amp; roles</h1></EuiTitle>
      <EuiSpacer />
      {isPending && <EuiLoadingChart size="xl" />}
      {isError && <EuiText color="danger">{error.message}</EuiText>}
      {data && (
        <EuiPanel>
          <EuiBasicTable<UserItem> items={data.users} columns={columns} rowHeader="username" noItemsMessage="No users yet." />
        </EuiPanel>
      )}

      {session?.user?.role === 'admin' && (
        <>
          <EuiSpacer />
          <EuiPanel>
            <EuiTitle size="xs"><h2>Add a user</h2></EuiTitle>
            <EuiSpacer size="s" />
            <EuiForm component="div">
              <EuiFormRow label="Username"><EuiFieldText value={form.username} onChange={(e) => setForm({ ...form, username: e.target.value })} /></EuiFormRow>
              <EuiFormRow label="Password" helpText="At least 12 characters.">
                <EuiFieldText type="password" value={form.password} onChange={(e) => setForm({ ...form, password: e.target.value })} />
              </EuiFormRow>
              <EuiFormRow label="Confirm password">
                <EuiFieldText type="password" value={form.confirm} onChange={(e) => setForm({ ...form, confirm: e.target.value })} />
              </EuiFormRow>
              <EuiFormRow label="Role">
                <EuiSelect options={[{ value: 'viewer', text: 'Viewer' }, { value: 'admin', text: 'Admin' }]}
                  value={form.role} onChange={(e) => setForm({ ...form, role: e.target.value })} />
              </EuiFormRow>
              <EuiFormRow>
                <EuiSwitch label="Require a password change at first login" checked={form.mustChange}
                  onChange={(e) => setForm({ ...form, mustChange: e.target.checked })} />
              </EuiFormRow>
              {add.isError && <EuiText color="danger" size="s">{add.error.message}</EuiText>}
              <EuiSpacer size="s" />
              <EuiButton isLoading={add.isPending} onClick={() => add.mutate(form, {
                onSuccess: () => setForm({ username: '', password: '', confirm: '', role: 'viewer', mustChange: false }),
              })}>
                Create user
              </EuiButton>
            </EuiForm>
          </EuiPanel>
        </>
      )}
    </>
  )
}
