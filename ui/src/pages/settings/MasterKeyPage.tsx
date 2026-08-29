import { EuiLoadingChart, EuiPanel, EuiSpacer, EuiText, EuiTitle } from '@elastic/eui'
import { useMasterKey } from '../../api/queries/settings'
import { SettingsSubNav } from './SettingsSubNav'

// Ported from internal/web/templates/masterkey.html against
// GET /api/ui/settings/masterkey — the key itself is never rendered, only
// where it lives and how many rows depend on it.
export function MasterKeyPage() {
  const { data, isPending, isError, error } = useMasterKey()

  return (
    <>
      <SettingsSubNav />
      <EuiTitle size="m"><h1>Master key</h1></EuiTitle>
      <EuiSpacer />
      {isPending && <EuiLoadingChart size="xl" />}
      {isError && <EuiText color="danger">{error.message}</EuiText>}
      {data && (
        <EuiPanel>
          <EuiText size="s">
            <p>Path: <code>{data.path}</code></p>
            {data.dataDir && <p>Data directory: <code>{data.dataDir}</code></p>}
            <p>{data.credentialsEncrypted} credential(s), {data.apiKeysEncrypted} API key(s) and {data.jobLogsEncrypted} job log(s) are encrypted with this key.</p>
          </EuiText>
          <EuiSpacer size="s" />
          <EuiText size="s" color="subdued">
            The Master Key is read from disk on startup and held in memory — it is never stored in the database and
            never rendered by this page. Rotating it means re-encrypting every row above with a new key, then
            restarting nagipath with the new key file in place.
          </EuiText>
        </EuiPanel>
      )}
    </>
  )
}
