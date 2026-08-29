import { useState } from 'react'
import {
  EuiBasicTable,
  EuiButton,
  EuiFieldText,
  EuiForm,
  EuiFormRow,
  EuiLoadingChart,
  EuiPanel,
  EuiSelect,
  EuiSpacer,
  EuiText,
  EuiTitle,
} from '@elastic/eui'
import { useSession } from '../../api/queries/session'
import { useAddCredential, useCredentials } from '../../api/queries/settings'
import type { CredentialItem } from '../../api/pb/nagipath/api/v1/settings_pb'
import { SettingsSubNav } from './SettingsSubNav'

const authKinds = [
  { value: 'private_key', text: 'SSH private key' },
  { value: 'ssh_certificate', text: 'SSH certificate' },
  { value: 'username_password', text: 'Username + password' },
  { value: 'ldap', text: 'LDAP' },
  { value: 'kerberos', text: 'Kerberos' },
  { value: 'cyberark', text: 'CyberArk reference' },
]

// Ported from internal/web/templates/credentials.html against
// GET/POST /api/ui/settings/credentials (internal/api/settings.go).
export function CredentialsPage() {
  const { data: session } = useSession()
  const { data, isPending, isError, error } = useCredentials('')
  const add = useAddCredential()
  const [form, setForm] = useState({
    name: '', username: '', authKind: 'private_key', privateKey: '', passphrase: '',
    certificate: '', password: '', externalRef: '',
  })

  const columns = [
    { field: 'name', name: 'Name' },
    { field: 'authKind', name: 'Type' },
    { field: 'username', name: 'Username' },
    { field: 'fingerprint', name: 'Fingerprint', render: (v: string) => v || '—' },
    { field: 'nodeCount', name: 'Nodes' },
    { field: 'lastUsed', name: 'Last used', render: (v: string) => (v ? new Date(v).toLocaleString() : '—') },
  ]

  return (
    <>
      <SettingsSubNav />
      <EuiTitle size="m"><h1>Credentials</h1></EuiTitle>
      <EuiSpacer />

      {isPending && <EuiLoadingChart size="xl" />}
      {isError && <EuiText color="danger">{error.message}</EuiText>}
      {data && (
        <EuiPanel>
          <EuiText size="s"><strong>{data.credentials.length}</strong> credential(s)</EuiText>
          <EuiSpacer size="s" />
          <EuiBasicTable<CredentialItem> items={data.credentials} columns={columns} rowHeader="name"
            noItemsMessage="No credentials stored yet." />
        </EuiPanel>
      )}

      {session?.user?.role === 'admin' && (
        <>
          <EuiSpacer />
          <EuiPanel>
            <EuiTitle size="xs"><h2>Add a credential</h2></EuiTitle>
            <EuiSpacer size="s" />
            <EuiForm component="div">
              <EuiFormRow label="Name"><EuiFieldText value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} /></EuiFormRow>
              <EuiFormRow label="Type">
                <EuiSelect options={authKinds} value={form.authKind} onChange={(e) => setForm({ ...form, authKind: e.target.value })} />
              </EuiFormRow>
              <EuiFormRow label="Username"><EuiFieldText value={form.username} onChange={(e) => setForm({ ...form, username: e.target.value })} /></EuiFormRow>

              {(form.authKind === 'private_key' || form.authKind === 'ssh_certificate') && (
                <EuiFormRow label="Private key">
                  <textarea rows={6} style={{ width: '100%' }} value={form.privateKey}
                    onChange={(e) => setForm({ ...form, privateKey: e.target.value })} />
                </EuiFormRow>
              )}
              {form.authKind === 'ssh_certificate' && (
                <EuiFormRow label="Certificate">
                  <textarea rows={4} style={{ width: '100%' }} value={form.certificate}
                    onChange={(e) => setForm({ ...form, certificate: e.target.value })} />
                </EuiFormRow>
              )}
              {(form.authKind === 'private_key' || form.authKind === 'ssh_certificate') && (
                <EuiFormRow label="Passphrase (optional)">
                  <EuiFieldText type="password" value={form.passphrase} onChange={(e) => setForm({ ...form, passphrase: e.target.value })} />
                </EuiFormRow>
              )}
              {(form.authKind === 'username_password' || form.authKind === 'ldap' || form.authKind === 'kerberos') && (
                <EuiFormRow label="Password">
                  <EuiFieldText type="password" value={form.password} onChange={(e) => setForm({ ...form, password: e.target.value })} />
                </EuiFormRow>
              )}
              {(form.authKind === 'ldap' || form.authKind === 'kerberos' || form.authKind === 'cyberark') && (
                <EuiFormRow label="External reference (optional)">
                  <EuiFieldText value={form.externalRef} onChange={(e) => setForm({ ...form, externalRef: e.target.value })} />
                </EuiFormRow>
              )}

              {add.isError && <EuiText color="danger" size="s">{add.error.message}</EuiText>}
              <EuiSpacer size="s" />
              <EuiButton isLoading={add.isPending} onClick={() => add.mutate(form, {
                onSuccess: () => setForm({ name: '', username: '', authKind: 'private_key', privateKey: '', passphrase: '', certificate: '', password: '', externalRef: '' }),
              })}>
                Store credential
              </EuiButton>
            </EuiForm>
          </EuiPanel>
        </>
      )}
    </>
  )
}
