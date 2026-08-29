import {
  EuiBadge,
  EuiBasicTable,
  EuiFieldSearch,
  EuiFlexGroup,
  EuiFlexItem,
  EuiLink,
  EuiLoadingChart,
  EuiPageTemplate,
  EuiPanel,
  EuiSpacer,
  EuiStat,
  EuiText,
  EuiTitle,
} from '@elastic/eui'
import { useSearchParams } from 'react-router-dom'
import { useSites } from '../../api/queries/sites'
import type { SiteListRow } from '../../api/types'

const stateColor: Record<string, 'success' | 'warning' | 'danger' | 'default'> = {
  OK: 'success',
}

// Ported from internal/web/templates/sites.html against GET /api/ui/sites.
// The old page's inline "click a row, preview its variants without leaving
// the list" panel isn't reproduced — the full detail page (SiteDetailPage)
// is one click away at /sites/{name} and covers the same ground.
export function SitesListPage() {
  const [params, setParams] = useSearchParams()
  const q = params.get('q') ?? ''
  const { data, isPending, isError, error } = useSites({ q })

  if (isPending) return <EuiPageTemplate.EmptyPrompt icon={<EuiLoadingChart size="xl" />} title={<h2>Loading sites…</h2>} />
  if (isError) return <EuiPageTemplate.EmptyPrompt iconType="alert" color="danger" title={<h2>Could not load sites</h2>} body={<p>{error.message}</p>} />

  const columns = [
    {
      field: 'Name',
      name: 'Hostname',
      render: (name: string) => <EuiLink href={`/sites/${encodeURIComponent(name)}`}>{name}</EuiLink>,
    },
    { field: 'Nodes', name: 'Nodes' },
    { field: 'ListenerSummary', name: 'Listener' },
    { field: 'CertSubject', name: 'Certificate' },
    { field: 'Routes', name: 'Routes' },
    { field: 'Variants', name: 'Config', render: (v: number) => (v > 1 ? <EuiBadge color="warning">{v} variants</EuiBadge> : 'IDENTICAL') },
    {
      field: 'State',
      name: 'State',
      render: (state: string) => <EuiBadge color={stateColor[state] ?? 'default'}>{state}</EuiBadge>,
    },
  ]

  return (
    <>
      <EuiTitle size="m">
        <h1>Sites</h1>
      </EuiTitle>
      <EuiSpacer />

      <EuiFlexGroup>
        <EuiFlexItem grow={false}><EuiPanel><EuiStat title={data.Stats.Hostnames} description="Hostnames" titleColor="primary" /></EuiPanel></EuiFlexItem>
        <EuiFlexItem grow={false}><EuiPanel><EuiStat title={data.Stats.TLSTerminated} description="TLS terminated" titleColor="primary" /></EuiPanel></EuiFlexItem>
        <EuiFlexItem grow={false}><EuiPanel><EuiStat title={data.Stats.Plaintext} description="Plaintext" titleColor={data.Stats.Plaintext > 0 ? 'warning' : 'primary'} /></EuiPanel></EuiFlexItem>
        <EuiFlexItem grow={false}><EuiPanel><EuiStat title={data.Stats.ExpiringCerts} description="Certs expiring" titleColor={data.Stats.ExpiringCerts > 0 ? 'warning' : 'primary'} /></EuiPanel></EuiFlexItem>
        <EuiFlexItem grow={false}><EuiPanel><EuiStat title={data.Stats.VariantHosts} description="Variant hosts" titleColor="primary" /></EuiPanel></EuiFlexItem>
      </EuiFlexGroup>
      <EuiSpacer />

      <EuiFieldSearch
        placeholder="filter by hostname or listener"
        defaultValue={q}
        onSearch={(v) => setParams((p) => { if (v) p.set('q', v); else p.delete('q'); return p })}
        style={{ maxWidth: 360 }}
      />
      <EuiSpacer size="s" />

      <EuiPanel>
        <EuiBasicTable<SiteListRow>
          items={data.Rows}
          columns={columns}
          rowHeader="Name"
          noItemsMessage={
            <EuiText size="s" color="subdued">
              {q ? 'No sites match this filter.' : 'No sites collected yet.'}
            </EuiText>
          }
        />
      </EuiPanel>
    </>
  )
}
