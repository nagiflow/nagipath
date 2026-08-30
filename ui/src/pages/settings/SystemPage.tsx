import { useDiagnostics } from '../../api/queries/settings'
import { PanelHeader } from '../../components/shared/PanelHeader'
import { Badge, Button, CallOut, Kv, Loading, Panel } from '../../components/ui'
import { SettingsLayout } from './SettingsLayout'

// Ported from internal/web/templates/diagnostics.html against
// GET /api/settings/system, laid out as design/'s screen 8j. The bundle download
// stays a plain link to GET /settings/system/bundle
// (internal/web/diagnostics.go): it is a text/plain attachment, not JSON, so it
// never needed a proto response.
export function SystemPage() {
  const { data, isPending, isError, error } = useDiagnostics()
  // design/'s badge. The two things this page can see going wrong: a missing
  // master key (nothing can be decrypted) and a database the binary disagrees
  // with. Everything else is reported as a value, not a state.
  const healthy = !!data && data.masterKeyPresent && data.migrationsApplied === data.migrationsExpected

  return (
    <SettingsLayout title="System" actions={<span className="m mus">read-only · nothing on this page changes state</span>}>
      {isPending && <Loading label="Loading diagnostics…" />}
      {isError && <div className="m" style={{ color: '#a1231c' }}>{error.message}</div>}
      {data && (
        <Panel>
          <PanelHeader
            title="This install"
            actions={<Badge cls={healthy ? 'v' : 'd'}>{healthy ? 'HEALTHY' : 'NEEDS ATTENTION'}</Badge>}
          />
          <Kv labelWidth={140} rows={[
            ['version', `nagipath ${data.version} · Go ${data.goVersion}`],
            ['uptime', `${Math.floor(Number(data.uptimeSeconds) / 3600)}h ${Math.floor((Number(data.uptimeSeconds) % 3600) / 60)}m`],
            ['database', <>{data.dbPath} <span className="mus">{(Number(data.dbSizeBytes) / 1e6).toFixed(1)} MB</span></>],
            ['master key', <>{data.masterKeyPath} <span className="mus">{data.masterKeyPresent ? `present, mode ${data.masterKeyMode}` : 'missing'}</span></>],
            ['listen', `${data.listenAddr} · TLS ${data.tlsEnabled ? 'enabled' : 'disabled'}${data.demoMode ? ' · DEMO MODE' : ''}`],
            ['license', `${data.licenseStatus} — ${data.licenseMessage}`],
            ['migrations', `${data.migrationsApplied} of ${data.migrationsExpected} expected`],
          ]} />
          <div style={{ marginTop: 8 }}>
            <CallOut title="Diagnostics bundle">
              <p>Version, uptime, database stats, collection stats, migrations and recent logs — never credentials, key
                material, session/token values, certificates or Probe tokens.</p>
              <div style={{ marginTop: 6 }}>
                <Button small href="/settings/system/bundle">Download bundle</Button>
              </div>
            </CallOut>
          </div>
        </Panel>
      )}
    </SettingsLayout>
  )
}
