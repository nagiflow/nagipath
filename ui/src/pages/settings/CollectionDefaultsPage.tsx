import { useState } from 'react'
import { EuiButton, EuiFieldNumber, EuiForm, EuiFormRow, EuiLoadingChart, EuiPanel, EuiSelect, EuiSpacer, EuiText, EuiTitle } from '@elastic/eui'
import { useSession } from '../../api/queries/session'
import { useCollectionDefaults, useSetCollectionDefaults } from '../../api/queries/settings'
import { SettingsSubNav } from './SettingsSubNav'

// Ported from internal/web/templates/collection_defaults.html against
// GET/POST /api/ui/settings/collection-defaults.
export function CollectionDefaultsPage() {
  const { data: session } = useSession()
  const { data, isPending, isError, error } = useCollectionDefaults()
  const setDefaults = useSetCollectionDefaults()
  const [form, setForm] = useState<Record<string, number>>({})

  if (isPending) return <EuiLoadingChart size="xl" />
  if (isError) return <EuiText color="danger">{error.message}</EuiText>
  if (!data) return null

  const field = (key: string, fallback: number) => form[key] ?? fallback

  return (
    <>
      <SettingsSubNav />
      <EuiTitle size="m"><h1>Collection defaults</h1></EuiTitle>
      <EuiSpacer />
      <EuiPanel>
        <EuiForm component="div">
          <EuiFormRow label="Default credential">
            <EuiSelect
              options={[{ value: '0', text: 'none' }, ...data.credentials.map((c) => ({ value: c.id.toString(), text: c.name }))]}
              value={String(form.defaultCredential ?? data.defaultCredential)}
              onChange={(e) => setForm({ ...form, defaultCredential: Number(e.target.value) })}
              disabled={session?.user?.role !== 'admin'}
            />
          </EuiFormRow>
          <EuiFormRow label="Collection interval (minutes)">
            <EuiFieldNumber value={field('intervalMinutes', data.intervalMinutes)} onChange={(e) => setForm({ ...form, intervalMinutes: Number(e.target.value) })} disabled={session?.user?.role !== 'admin'} />
          </EuiFormRow>
          <EuiFormRow label="Jitter (seconds)">
            <EuiFieldNumber value={field('jitterSeconds', data.jitterSeconds)} onChange={(e) => setForm({ ...form, jitterSeconds: Number(e.target.value) })} disabled={session?.user?.role !== 'admin'} />
          </EuiFormRow>
          <EuiFormRow label="SSH workers">
            <EuiFieldNumber value={field('sshWorkers', data.sshWorkers)} onChange={(e) => setForm({ ...form, sshWorkers: Number(e.target.value) })} disabled={session?.user?.role !== 'admin'} />
          </EuiFormRow>
          <EuiFormRow label="SSH command timeout (seconds)">
            <EuiFieldNumber value={field('commandTimeout', data.commandTimeout)} onChange={(e) => setForm({ ...form, commandTimeout: Number(e.target.value) })} disabled={session?.user?.role !== 'admin'} />
          </EuiFormRow>
          <EuiFormRow label="Max files per snapshot">
            <EuiFieldNumber value={field('maxFiles', data.maxFiles)} onChange={(e) => setForm({ ...form, maxFiles: Number(e.target.value) })} disabled={session?.user?.role !== 'admin'} />
          </EuiFormRow>
          <EuiFormRow label="Probe max redirects">
            <EuiFieldNumber value={field('probeRedirects', data.probeRedirects)} onChange={(e) => setForm({ ...form, probeRedirects: Number(e.target.value) })} disabled={session?.user?.role !== 'admin'} />
          </EuiFormRow>
          <EuiFormRow label="Probe log lookback (seconds)">
            <EuiFieldNumber value={field('probeLookback', data.probeLookback)} onChange={(e) => setForm({ ...form, probeLookback: Number(e.target.value) })} disabled={session?.user?.role !== 'admin'} />
          </EuiFormRow>

          {session?.user?.role === 'admin' && (
            <>
              {setDefaults.isError && <EuiText color="danger" size="s">{setDefaults.error.message}</EuiText>}
              <EuiSpacer size="s" />
              <EuiButton isLoading={setDefaults.isPending} onClick={() => setDefaults.mutate(form)}>
                Save collection defaults
              </EuiButton>
            </>
          )}
        </EuiForm>
      </EuiPanel>
    </>
  )
}
