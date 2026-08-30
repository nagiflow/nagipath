import { useState } from 'react'
import { useSession } from '../../api/queries/session'
import { useRetention, useRunRetention, useSetRetention } from '../../api/queries/settings'
import { PanelHeader } from '../../components/shared/PanelHeader'
import { Button, Loading, Panel, StatRow } from '../../components/ui'
import { SettingsLayout } from './SettingsLayout'

function bytes(n: bigint | number): string {
  const v = Number(n)
  return v >= 1e9 ? `${(v / 1e9).toFixed(1)} GB` : `${(v / 1e6).toFixed(1)} MB`
}

// design/'s setting row on screens 8c and 8f: a 130px label, a narrow number
// field, the unit as quiet text.
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

// Ported from internal/web/templates/retention.html against
// GET/POST /api/settings/retention and POST /api/settings/retention/prune
// (internal/api/settingsservice.go), laid out as design/'s screen 8f: the size
// tiles, the windows beside the exceptions that override them, and the last
// prune's per-class figures.
// Omitted from that screen: the per-class byte figures under each tile and
// "of 100 GB volume" (RetentionStats measures the whole index once — it does not
// attribute bytes to snapshots vs job logs, and nothing reads the volume size),
// the "Next prune … est. frees 640 MB" tile (prune runs hourly from
// cmd/nagipath's housekeeping loop, which publishes no next-run time and
// estimates nothing), the "Drift decisions ∞" row (drift findings are recomputed,
// not retained on a window), the four exception checkboxes and "Warn at % of
// volume" (the only exception the pruner honours is the minimum snapshots kept
// per instance, shown here as the number it is), and "Prune history" (one
// previous run is stored, not a list).
export function RetentionPage() {
  const { data: session } = useSession()
  const { data, isPending, isError, error } = useRetention()
  const save = useSetRetention()
  const prune = useRunRetention()
  const [form, setForm] = useState<Record<string, number>>({})
  const isAdmin = session?.user?.role === 'admin'

  if (isPending) return <Loading label="Loading retention settings…" />
  if (isError) return <div className="m" style={{ color: '#a1231c' }}>{error.message}</div>
  if (!data) return null

  const dirty = Object.keys(form).length
  const val = (key: string, fallback: number) => form[key] ?? fallback
  const set = (key: string) => (v: number) => setForm({ ...form, [key]: v })
  const stats = data.stats
  const last = stats?.lastPrune

  const classes = last
    ? [
      { name: 'Snapshots', examined: last.examined?.snapshots, deleted: Number(last.deleted?.snapshots ?? 0n), kept: last.kept?.snapshots },
      { name: 'Job logs', examined: last.examined?.jobLogs, deleted: Number(last.deleted?.jobLogs ?? 0n), kept: last.kept?.jobLogs },
      { name: 'Probe records', examined: last.examined?.traces, deleted: Number(last.deleted?.probes ?? 0n), kept: last.kept?.traces },
      { name: 'Audit log', examined: last.examined?.auditEvents, deleted: Number(last.deleted?.auditEvents ?? 0n), kept: last.kept?.auditEvents },
      { name: 'Blobs', examined: undefined, deleted: Number(last.deleted?.blobs ?? 0n), kept: undefined },
    ]
    : []

  return (
    <SettingsLayout
      title="Retention"
      actions={
        <>
          <span className="m mus">
            {dirty > 0 ? `${dirty} unsaved change${dirty === 1 ? '' : 's'}` : 'prune runs hourly'}
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

        <StatRow stats={[
          { label: 'Index size', value: bytes(stats?.indexSizeBytes ?? 0n), sub: 'database file on disk' },
          { label: 'Snapshots', value: stats?.snapshots ?? 0, sub: 'stored captures' },
          { label: 'Job logs', value: stats?.jobLogs ?? 0, sub: 'collection runs' },
          { label: 'Traces', value: stats?.traces ?? 0, sub: 'recorded lookups' },
          {
            label: 'Last prune',
            value: last ? bytes(last.freedBytes) : '—',
            sub: last ? `${new Date(last.ranAt).toLocaleString()} · ${last.durationSeconds.toFixed(1)} s` : 'no manual run recorded',
          },
        ]} />

        <div className="row" style={{ alignItems: 'flex-start' }}>
          <Panel style={{ flex: 1, minWidth: 0 }}>
            <div className="lbl" style={{ marginBottom: 8 }}>Keep for</div>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 7 }}>
              <NumRow label="Snapshots" unit="days · 0 keeps everything" disabled={!isAdmin}
                value={val('snapshotDays', data.snapshotDays)} onChange={set('snapshotDays')} />
              <NumRow label="Job logs" unit="days" disabled={!isAdmin}
                value={val('jobLogDays', data.jobLogDays)} onChange={set('jobLogDays')} />
              <NumRow label="Probe records" unit="days · includes trace results" disabled={!isAdmin}
                value={val('probeDays', data.probeDays)} onChange={set('probeDays')} />
              <NumRow label="Audit log" unit="days" disabled={!isAdmin}
                value={val('auditDays', data.auditDays)} onChange={set('auditDays')} />
            </div>
          </Panel>

          <Panel style={{ flex: 1, minWidth: 0 }}>
            <div className="lbl" style={{ marginBottom: 8 }}>Exceptions</div>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 7 }}>
              <NumRow label="Always keep" unit="snapshots per process, however old" disabled={!isAdmin}
                value={val('minPerInstance', data.minPerInstance)} onChange={set('minPerInstance')} />
              <span className="m mus">
                A window of 0 keeps that class forever. The floor above outranks the snapshot window, so a process
                that has not been collected in months still has its last captures — without it, a quiet node would
                age out of the index entirely.
              </span>
              <span className="m mus">
                A blob is deleted only once the last snapshot file referencing it is gone, so freed bytes lag the
                snapshot count by one pass.
              </span>
            </div>
          </Panel>
        </div>

        <Panel z>
          <PanelHeader
            title="Last prune"
            meta={last ? `${new Date(last.ranAt).toLocaleString()} · ${last.durationSeconds.toFixed(1)} s` : 'never run by hand'}
            actions={isAdmin && (
              <Button small loading={prune.isPending} onClick={() => prune.mutate()}>Run now</Button>
            )}
          />
          {prune.isError && <div className="m" style={{ color: '#a1231c', padding: '6px 12px' }}>{prune.error.message}</div>}
          {last ? (
            <table className="t">
              <thead>
                <tr>
                  <th style={{ width: 200 }}>Class</th>
                  <th style={{ width: 120 }}>Examined</th>
                  <th style={{ width: 120 }}>Deleted</th>
                  <th style={{ width: 160 }}>Kept by exception</th>
                  <th>Freed</th>
                </tr>
              </thead>
              <tbody>
                {classes.map((c, i) => (
                  <tr key={c.name} className={i % 2 === 1 ? 'zz' : undefined}>
                    <td className="m">{c.name}</td>
                    <td className={c.examined === undefined ? 'm mus' : 'm mu'}>{c.examined === undefined ? '—' : c.examined.toLocaleString()}</td>
                    <td className="m mu">{c.deleted.toLocaleString()}</td>
                    <td className={c.kept === undefined ? 'm mus' : 'm mu'}>{c.kept === undefined ? '—' : c.kept.toLocaleString()}</td>
                    <td className="m mus">{c.name === 'Blobs' ? bytes(last.freedBytes) : '—'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          ) : (
            <div className="m mus" style={{ padding: '10px 12px' }}>
              Prune has not been run by hand. The hourly pass does the same work but records no figures.
            </div>
          )}
        </Panel>
      </div>
    </SettingsLayout>
  )
}
