import { useState } from 'react'
import { EuiButton, EuiCallOut, EuiForm, EuiFormRow, EuiLoadingChart, EuiPanel, EuiSpacer, EuiText, EuiTitle } from '@elastic/eui'
import { useSession } from '../../api/queries/session'
import { useInstallLicense, useLicense } from '../../api/queries/settings'
import { SettingsSubNav } from './SettingsSubNav'

// Ported from internal/web/templates/license.html against
// GET/POST /api/ui/settings/license. A viewer can read status; only an
// admin sees (and can submit) the install form.
export function LicensePage() {
  const { data: session } = useSession()
  const { data, isPending, isError, error } = useLicense()
  const install = useInstallLicense()
  const [text, setText] = useState('')

  return (
    <>
      <SettingsSubNav />
      <EuiTitle size="m"><h1>License</h1></EuiTitle>
      <EuiSpacer />
      {isPending && <EuiLoadingChart size="xl" />}
      {isError && <EuiText color="danger">{error.message}</EuiText>}
      {data && (
        <EuiPanel>
          {data.loaded ? (
            <EuiText size="s">
              <p>Customer: {data.customer}</p>
              <p>Edition: {data.edition}</p>
              <p>Node ceiling: {data.nodeCeiling}</p>
              <p>Expires: {new Date(data.expiry).toLocaleDateString()}</p>
              <p>Nodes in use: {data.nodeCount}</p>
            </EuiText>
          ) : (
            <EuiText size="s" color="subdued">No license is currently loaded.</EuiText>
          )}
          <EuiSpacer size="s" />
          <EuiCallOut size="s" title={data.status} color={data.status === 'ok' ? 'success' : data.status === 'expired' ? 'danger' : 'warning'}>
            {data.message}
          </EuiCallOut>
          {data.state && (
            <>
              <EuiSpacer size="s" />
              <EuiText size="s" color="subdued">
                Last installed: {data.state.customer} ({data.state.edition}), by {data.state.installedByUsername || 'unknown'} on{' '}
                {data.state.lastEvaluatedAt ? new Date(data.state.lastEvaluatedAt).toLocaleString() : '—'}
              </EuiText>
            </>
          )}
        </EuiPanel>
      )}

      {session?.user?.role === 'admin' && (
        <>
          <EuiSpacer />
          <EuiPanel>
            <EuiTitle size="xs"><h2>Install a license</h2></EuiTitle>
            <EuiSpacer size="s" />
            <EuiForm component="div">
              <EuiFormRow label="License text" fullWidth>
                <textarea rows={8} style={{ width: '100%' }} value={text} onChange={(e) => setText(e.target.value)} />
              </EuiFormRow>
              {install.isError && <EuiText color="danger" size="s">{install.error.message}</EuiText>}
              <EuiSpacer size="s" />
              <EuiButton isLoading={install.isPending} onClick={() => install.mutate(text, { onSuccess: () => setText('') })}>
                Install license
              </EuiButton>
            </EuiForm>
          </EuiPanel>
        </>
      )}
    </>
  )
}
