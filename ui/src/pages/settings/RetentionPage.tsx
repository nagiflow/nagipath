import { useState } from 'react'
import { EuiButton, EuiFieldNumber, EuiForm, EuiFormRow, EuiLoadingChart, EuiPanel, EuiSpacer, EuiText, EuiTitle } from '@elastic/eui'
import { useSession } from '../../api/queries/session'
import { useRetention, useRunRetention, useSetRetention } from '../../api/queries/settings'
import { SettingsSubNav } from './SettingsSubNav'

// Ported from internal/web/templates/retention.html against
// GET/POST /api/ui/settings/retention and POST /api/ui/settings/retention/prune.
export function RetentionPage() {
  const { data: session } = useSession()
  const { data, isPending, isError, error } = useRetention()
  const setRetention = useSetRetention()
  const prune = useRunRetention()
  const [form, setForm] = useState<Record<string, number>>({})

  const field = (key: keyof typeof form, fallback: number | undefined) => form[key] ?? fallback ?? 0

  return (
    <>
      <SettingsSubNav />
      <EuiTitle size="m"><h1>Retention</h1></EuiTitle>
      <EuiSpacer />
      {isPending && <EuiLoadingChart size="xl" />}
      {isError && <EuiText color="danger">{error.message}</EuiText>}
      {data && (
        <>
          <EuiPanel>
            <EuiText size="s">
              <p>Index size: {(Number(data.stats?.indexSizeBytes ?? 0n) / 1e6).toFixed(1)} MB</p>
              <p>{data.stats?.snapshots ?? 0} snapshot(s), {data.stats?.jobLogs ?? 0} job log(s), {data.stats?.traces ?? 0} trace(s) on disk.</p>
            </EuiText>
            {data.stats?.lastPrune && (
              <EuiText size="s" color="subdued">
                Last manual prune: {new Date(data.stats.lastPrune.ranAt).toLocaleString()}, freed {(Number(data.stats.lastPrune.freedBytes) / 1e6).toFixed(1)} MB
              </EuiText>
            )}
          </EuiPanel>
          <EuiSpacer />

          {session?.user?.role === 'admin' && (
            <EuiPanel>
              <EuiForm component="div">
                <EuiFormRow label="Snapshot retention (days)">
                  <EuiFieldNumber value={field('snapshotDays', data.snapshotDays)} onChange={(e) => setForm({ ...form, snapshotDays: Number(e.target.value) })} />
                </EuiFormRow>
                <EuiFormRow label="Minimum snapshots kept per instance">
                  <EuiFieldNumber value={field('minPerInstance', data.minPerInstance)} onChange={(e) => setForm({ ...form, minPerInstance: Number(e.target.value) })} />
                </EuiFormRow>
                <EuiFormRow label="Job log retention (days)">
                  <EuiFieldNumber value={field('jobLogDays', data.jobLogDays)} onChange={(e) => setForm({ ...form, jobLogDays: Number(e.target.value) })} />
                </EuiFormRow>
                <EuiFormRow label="Probe retention (days)">
                  <EuiFieldNumber value={field('probeDays', data.probeDays)} onChange={(e) => setForm({ ...form, probeDays: Number(e.target.value) })} />
                </EuiFormRow>
                <EuiFormRow label="Audit log retention (days)">
                  <EuiFieldNumber value={field('auditDays', data.auditDays)} onChange={(e) => setForm({ ...form, auditDays: Number(e.target.value) })} />
                </EuiFormRow>
                {setRetention.isError && <EuiText color="danger" size="s">{setRetention.error.message}</EuiText>}
                <EuiSpacer size="s" />
                <EuiButton isLoading={setRetention.isPending} onClick={() => setRetention.mutate(form)}>
                  Save retention windows
                </EuiButton>
                <EuiSpacer />
                {prune.isError && <EuiText color="danger" size="s">{prune.error.message}</EuiText>}
                <EuiButton color="danger" isLoading={prune.isPending} onClick={() => prune.mutate()}>
                  Run prune now
                </EuiButton>
              </EuiForm>
            </EuiPanel>
          )}
        </>
      )}
    </>
  )
}
