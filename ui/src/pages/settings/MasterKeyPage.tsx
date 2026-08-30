import { useMasterKey } from '../../api/queries/settings'
import { Badge, Loading, Panel, StatRow } from '../../components/ui'
import { SettingsLayout } from './SettingsLayout'

// Ported from internal/web/templates/masterkey.html against
// GET /api/settings/masterkey (internal/api/settingsservice.go), laid out as
// design/'s screen 8e: the state tiles, where the key comes from, and what
// depends on it. The key itself is never rendered — only its path.
// Omitted from that screen: "Rotate key"/"Download recovery kit" (there is no
// rotation path — internal/keys reads one key file at startup and refuses to
// overwrite it, so rotating means re-encrypting every row offline and restarting
// with the new file), the AWS KMS / Vault transit key sources and their key
// id/auth/last-check rows (internal/keys.Load takes a hex value or a key file;
// no external provider is wired), the "Key age … policy 365 d" tile (nothing
// records when the key was created), and the "Key history" table (key events are
// not recorded — the file is created once by `nagipath keygen`).
export function MasterKeyPage() {
  const { data, isPending, isError, error } = useMasterKey()

  return (
    <SettingsLayout
      title="Master key"
      actions={<span className="m mus">encrypts credentials, API keys and job logs at rest</span>}
    >
      {isPending && <Loading label="Loading…" />}
      {isError && <div className="m" style={{ color: '#a1231c' }}>{error.message}</div>}
      {data && (
        <div className="col">
          <StatRow stats={[
            { label: 'State', value: <span style={{ color: '#00726b', fontSize: 17 }}>Loaded</span>, sub: 'read at startup, held in memory' },
            { label: 'Source', value: <span style={{ fontSize: 17 }}>Key file</span>, sub: data.dataDir || data.path },
            {
              label: 'Encrypted items',
              value: data.credentialsEncrypted + data.apiKeysEncrypted + data.jobLogsEncrypted,
              sub: 'credentials · api keys · job logs',
            },
          ]} />

          <div className="row" style={{ alignItems: 'flex-start' }}>
            <Panel style={{ flex: 1, minWidth: 0 }}>
              <div className="lbl" style={{ marginBottom: 8 }}>Key source</div>
              <div className="fct"><input type="radio" className="rd" checked readOnly disabled /><span className="m">Local key file</span><span className="c">read once at startup</span></div>
              <div className="fct"><input type="radio" className="rd" readOnly disabled /><span className="m mus">External KMS</span></div>
              <div className="fct"><input type="radio" className="rd" readOnly disabled /><span className="m mus">HashiCorp Vault transit</span></div>
              <div className="kv m" style={{ display: 'grid', gridTemplateColumns: '96px 1fr', gap: '4px 8px', marginTop: 9 }}>
                <span className="mus">path</span><span>{data.path}</span>
                {data.dataDir && <><span className="mus">directory</span><span>{data.dataDir}</span></>}
                <span className="mus">contents</span><span className="mus">never read back · never rendered</span>
              </div>
            </Panel>

            <Panel style={{ flex: 1, minWidth: 0 }}>
              <div className="lbl" style={{ marginBottom: 8 }}>Encrypted with this key</div>
              <div className="fct"><span className="m">Credentials</span><span className="c">{data.credentialsEncrypted} records</span><Badge cls="v">OK</Badge></div>
              <div className="fct"><span className="m">API keys</span><span className="c">{data.apiKeysEncrypted} records</span><Badge cls="v">OK</Badge></div>
              <div className="fct"><span className="m">Job logs</span><span className="c">{data.jobLogsEncrypted} records</span><Badge cls="v">OK</Badge></div>
              <div className="fct"><span className="m">Snapshots</span><span className="c">not encrypted — configuration only</span><Badge cls="i">N/A</Badge></div>
              <div className="m mus" style={{ marginTop: 7 }}>
                Each ciphertext is bound to its own row and column, so a record cannot be moved to another row and
                still decrypt. Losing the key file makes every secret above unrecoverable, by design — replacing it
                means re-entering the secrets, not decrypting the old ones.
              </div>
            </Panel>
          </div>
        </div>
      )}
    </SettingsLayout>
  )
}
