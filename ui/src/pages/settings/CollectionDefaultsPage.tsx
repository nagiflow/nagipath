import { useState } from 'react'
import { useSession } from '../../api/queries/session'
import { useCollectionDefaults, useSetCollectionDefaults } from '../../api/queries/settings'
import { Button, Loading, Panel, Select } from '../../components/ui'
import { SettingsLayout } from './SettingsLayout'

// design/'s setting row on screen 8c: a 130px label, a narrow number field and
// the unit as quiet text after it. The allowed range lives in the unit text
// because SetCollectionDefaults rejects anything outside it.
function NumRow({ label, value, unit, onChange, disabled }: {
  label: string
  value: number
  unit: string
  onChange: (v: number) => void
  disabled?: boolean
}) {
  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
      <span className="m mu" style={{ width: 130, flex: 'none' }}>{label}</span>
      <span className="fld" style={{ width: 80, flex: 'none' }}>
        <input type="number" value={value} disabled={disabled} onChange={(e) => onChange(Number(e.target.value))} />
      </span>
      <span className="m mus">{unit}</span>
    </div>
  )
}

// Ported from internal/web/templates/collection_defaults.html against
// GET/POST /api/settings/collection-defaults (internal/api/settingsservice.go),
// laid out as design/'s screen 8c: the Discard/Save pair in the title bar and
// the settings grouped into panels by what they govern.
// Omitted from that screen: the "Method" panel's radios and checkboxes (there is
// no collection-method setting — collection always runs vendor tooling and falls
// back to reading files, which is why "degraded" is a first-class outcome rather
// than a policy), the whole "Schedules" panel with its per-target overrides,
// drag order, windows and "Dry run coverage" (collection runs on one global
// interval with jitter; there is no schedule table to order or scope), and "Max
// config size … MB per node" (the cap is on the number of files in a snapshot,
// not their bytes — shown here as it actually is).
export function CollectionDefaultsPage() {
  const { data: session } = useSession()
  const { data, isPending, isError, error } = useCollectionDefaults()
  const save = useSetCollectionDefaults()
  const [form, setForm] = useState<Record<string, number>>({})
  const isAdmin = session?.user?.role === 'admin'

  if (isPending) return <Loading label="Loading collection defaults…" />
  if (isError) return <div className="m" style={{ color: '#a1231c' }}>{error.message}</div>
  if (!data) return null

  const dirty = Object.keys(form).length
  const val = (key: keyof typeof form, fallback: number) => form[key] ?? fallback
  const set = (key: string) => (v: number) => setForm({ ...form, [key]: v })

  return (
    <SettingsLayout
      title="Collection defaults"
      actions={
        <>
          <span className="m mus">
            {dirty > 0 ? `${dirty} unsaved change${dirty === 1 ? '' : 's'}` : 'applies to every node'}
          </span>
          {isAdmin && (
            <>
              <Button small disabled={dirty === 0} onClick={() => setForm({})}>Discard</Button>
              <Button small primary disabled={dirty === 0} loading={save.isPending}
                onClick={() => save.mutate(form, { onSuccess: () => setForm({}) })}>Save</Button>
            </>
          )}
        </>
      }
    >
      <div className="col">
        {save.isError && <div className="m" style={{ color: '#a1231c' }}>{save.error.message}</div>}

        {/* 45% basis, not flex:1: three panels on one line squeeze the unit text
            into four wrapped lines. Two per line matches design/'s 8c width. */}
        <div className="row" style={{ alignItems: 'flex-start', flexWrap: 'wrap' }}>
          <Panel style={{ flex: '1 1 45%', minWidth: 0 }}>
            <div className="lbl" style={{ marginBottom: 8 }}>Execution</div>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 7 }}>
              <NumRow label="Concurrency" unit="nodes in parallel · 1–64" disabled={!isAdmin}
                value={val('sshWorkers', data.sshWorkers)} onChange={set('sshWorkers')} />
              <NumRow label="SSH timeout" unit="seconds per command · 5–600" disabled={!isAdmin}
                value={val('commandTimeout', data.commandTimeout)} onChange={set('commandTimeout')} />
              <NumRow label="Max files" unit="per snapshot · 10–100,000" disabled={!isAdmin}
                value={val('maxFiles', data.maxFiles)} onChange={set('maxFiles')} />
            </div>
          </Panel>

          <Panel style={{ flex: '1 1 45%', minWidth: 0 }}>
            <div className="lbl" style={{ marginBottom: 8 }}>Schedule</div>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 7 }}>
              <NumRow label="Every" unit="minutes · 1–10,080" disabled={!isAdmin}
                value={val('intervalMinutes', data.intervalMinutes)} onChange={set('intervalMinutes')} />
              <NumRow label="Jitter" unit="seconds, spreads the fleet · 0–3,600" disabled={!isAdmin}
                value={val('jitterSeconds', data.jitterSeconds)} onChange={set('jitterSeconds')} />
              <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                <span className="m mu" style={{ width: 130, flex: 'none' }}>Credential</span>
                <Select
                  options={[{ value: '0', text: 'none' },
                    ...data.credentials.map((c) => ({ value: c.id.toString(), text: c.name }))]}
                  value={String(form.defaultCredential ?? data.defaultCredential)}
                  onChange={(v) => setForm({ ...form, defaultCredential: Number(v) })}
                  disabled={!isAdmin}
                />
                <span className="m mus">unless the node names one</span>
              </div>
            </div>
          </Panel>

          <Panel style={{ flex: '1 1 45%', minWidth: 0 }}>
            <div className="lbl" style={{ marginBottom: 8 }}>Probes</div>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 7 }}>
              <NumRow label="Max redirects" unit="followed per probe · 0–20" disabled={!isAdmin}
                value={val('probeRedirects', data.probeRedirects)} onChange={set('probeRedirects')} />
              <NumRow label="Log lookback" unit="seconds searched for the hit · 10–3,600" disabled={!isAdmin}
                value={val('probeLookback', data.probeLookback)} onChange={set('probeLookback')} />
              <span className="m mus">
                A probe sends one real request and then looks for it in the node's access log, which is why the
                lookback matters.
              </span>
            </div>
          </Panel>
        </div>
      </div>
    </SettingsLayout>
  )
}
