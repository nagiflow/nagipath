import { useState } from 'react'
import { useSession } from '../../api/queries/session'
import { useInstallLicense, useLicense } from '../../api/queries/settings'
import { PanelHeader } from '../../components/shared/PanelHeader'
import { Badge, Button, CallOut, Kv, Loading, Panel } from '../../components/ui'
import { SettingsLayout } from './SettingsLayout'

// Ported from internal/web/templates/license.html against
// GET/POST /api/settings/license, laid out as design/'s screen 8i. A viewer can
// read status; only an admin sees (and can submit) the install form.
export function LicensePage() {
  const { data: session } = useSession()
  const { data, isPending, isError, error } = useLicense()
  const install = useInstallLicense()
  const [text, setText] = useState('')

  return (
    <SettingsLayout
      title="License"
      actions={data?.loaded && (
        <span className="m mus">
          {data.nodeCount} / {data.nodeCeiling} nodes · expires {new Date(data.expiry).toLocaleDateString()}
        </span>
      )}
    >
      {isPending && <Loading label="Loading license…" />}
      {isError && <div className="m" style={{ color: '#a1231c' }}>{error.message}</div>}
      {data && (
        <Panel>
          <PanelHeader
            title="Current license"
            actions={<Badge cls={data.status === 'ok' ? 'v' : data.status === 'expired' ? 'r' : 'd'}>{data.status}</Badge>}
          />
          {data.loaded ? (
            <Kv labelWidth={110} rows={[
              ['customer', data.customer],
              ['edition', data.edition],
              ['node ceiling', data.nodeCeiling],
              ['expires', new Date(data.expiry).toLocaleDateString()],
              ['nodes in use', data.nodeCount],
            ]} />
          ) : (
            <p className="m mus">No license is currently loaded.</p>
          )}
          <div style={{ marginTop: 8 }}>
            <CallOut title={data.status} color={data.status === 'ok' ? 'success' : data.status === 'expired' ? 'danger' : 'warning'}>
              {data.message}
            </CallOut>
          </div>
          {data.state && (
            <p className="m mus" style={{ marginTop: 8 }}>
              Last installed: {data.state.customer} ({data.state.edition}), by {data.state.installedByUsername || 'unknown'} on{' '}
              {data.state.lastEvaluatedAt ? new Date(data.state.lastEvaluatedAt).toLocaleString() : '—'}
            </p>
          )}
        </Panel>
      )}

      {session?.user?.role === 'admin' && (
        <Panel style={{ marginTop: 10 }}>
          <PanelHeader title="Install a license" actions={<span className="m mus">admin only</span>} />
          <div className="col">
            <div className="col" style={{ gap: 4 }}>
              <span className="lbl">License text</span>
              <span className="fld f">
                <textarea rows={8} value={text} onChange={(e) => setText(e.target.value)} />
              </span>
            </div>
            {install.isError && <div className="m" style={{ color: '#a1231c' }}>{install.error.message}</div>}
            <div className="row">
              <Button primary loading={install.isPending} onClick={() => install.mutate(text, { onSuccess: () => setText('') })}>
                Install license
              </Button>
            </div>
          </div>
        </Panel>
      )}
    </SettingsLayout>
  )
}
