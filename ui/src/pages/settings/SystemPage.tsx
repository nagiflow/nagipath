import { EuiButton, EuiCallOut, EuiLoadingChart, EuiPanel, EuiSpacer, EuiText, EuiTitle } from '@elastic/eui'
import { useDiagnostics } from '../../api/queries/settings'
import { SettingsSubNav } from './SettingsSubNav'

// Ported from internal/web/templates/diagnostics.html against
// GET /api/ui/settings/system. The bundle download stays a plain link to
// GET /settings/system/bundle (internal/web/diagnostics.go): it is a
// text/plain attachment, not JSON, so it never needed a proto response.
export function SystemPage() {
  const { data, isPending, isError, error } = useDiagnostics()

  return (
    <>
      <SettingsSubNav />
      <EuiTitle size="m"><h1>System</h1></EuiTitle>
      <EuiSpacer />
      {isPending && <EuiLoadingChart size="xl" />}
      {isError && <EuiText color="danger">{error.message}</EuiText>}
      {data && (
        <EuiPanel>
          <EuiText size="s">
            <p>nagipath {data.version} · Go {data.goVersion}</p>
            <p>Uptime: {Math.floor(Number(data.uptimeSeconds) / 3600)}h {Math.floor((Number(data.uptimeSeconds) % 3600) / 60)}m</p>
            <p>Database: <code>{data.dbPath}</code> ({(Number(data.dbSizeBytes) / 1e6).toFixed(1)} MB)</p>
            <p>Master key: <code>{data.masterKeyPath}</code> {data.masterKeyPresent ? `(present, mode ${data.masterKeyMode})` : '(missing)'}</p>
            <p>Listen address: {data.listenAddr} · TLS {data.tlsEnabled ? 'enabled' : 'disabled'} {data.demoMode && '· DEMO MODE'}</p>
            <p>License: {data.licenseStatus} — {data.licenseMessage}</p>
            <p>Migrations applied: {data.migrationsApplied} of {data.migrationsExpected} expected</p>
          </EuiText>
          <EuiSpacer size="s" />
          <EuiCallOut size="s" title="Diagnostics bundle" iconType="download">
            <p>Version, uptime, database stats, collection stats, migrations and recent logs — never credentials, key
              material, session/token values, certificates or Probe tokens.</p>
            <EuiButton size="s" href="/settings/system/bundle">Download bundle</EuiButton>
          </EuiCallOut>
        </EuiPanel>
      )}
    </>
  )
}
