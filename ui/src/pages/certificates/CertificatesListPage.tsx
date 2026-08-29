import {
  EuiBadge,
  EuiBasicTable,
  EuiFlexGroup,
  EuiFlexItem,
  EuiLink,
  EuiLoadingChart,
  EuiPageTemplate,
  EuiPanel,
  EuiSelect,
  EuiSpacer,
  EuiText,
  EuiTitle,
} from '@elastic/eui'
import { useSearchParams } from 'react-router-dom'
import { useCertificates } from '../../api/queries/certificates'
import type { CertificateListItem } from '../../api/pb/nagipath/api/v1/certificates_pb'

function daysLeft(notAfter: string): number {
  return Math.floor((new Date(notAfter).getTime() - Date.now()) / (1000 * 60 * 60 * 24))
}

// Ported from internal/web/templates/certificates.html against GET
// /api/ui/certificates (internal/api/certificates.go).
export function CertificatesListPage() {
  const [params, setParams] = useSearchParams()
  const expires = params.get('expires') ?? ''
  const { data, isPending, isError, error } = useCertificates({ expires })

  if (isPending) return <EuiPageTemplate.EmptyPrompt icon={<EuiLoadingChart size="xl" />} title={<h2>Loading certificates…</h2>} />
  if (isError) return <EuiPageTemplate.EmptyPrompt iconType="alert" color="danger" title={<h2>Could not load certificates</h2>} body={<p>{error.message}</p>} />

  const columns = [
    {
      field: 'subjectCn',
      name: 'Subject',
      render: (cn: string, row: CertificateListItem) => <EuiLink href={`/certificates/${row.id}`}>{cn || '(no CN)'}</EuiLink>,
    },
    { field: 'issuerDn', name: 'Issuer' },
    {
      field: 'notAfter',
      name: 'Expires',
      render: (v: string) => {
        const days = daysLeft(v)
        const color = days < 0 ? 'danger' : days <= 30 ? 'warning' : 'default'
        return <EuiBadge color={color}>{days < 0 ? `expired ${-days}d ago` : `${days}d left`}</EuiBadge>
      },
    },
    { field: 'bindings', name: 'Bindings' },
    { field: 'hosts', name: 'Nodes' },
  ]

  return (
    <>
      <EuiTitle size="m"><h1>Certificates</h1></EuiTitle>
      <EuiSpacer size="s" />
      <EuiText size="s" color="subdued">{data.summary}</EuiText>
      <EuiSpacer />

      <EuiFlexGroup gutterSize="s">
        <EuiFlexItem grow={false}>
          <EuiSelect
            options={[
              { value: '', text: 'Expires: any time' },
              { value: 'expired', text: 'Expired' },
              { value: '7d', text: 'Within 7 days' },
              { value: '30d', text: 'Within 30 days' },
              { value: '90d', text: 'Within 90 days' },
            ]}
            value={expires}
            onChange={(e) => setParams((p) => { if (e.target.value) p.set('expires', e.target.value); else p.delete('expires'); return p })}
          />
        </EuiFlexItem>
      </EuiFlexGroup>
      <EuiSpacer size="s" />

      <EuiPanel>
        <EuiBasicTable<CertificateListItem>
          items={data.list}
          columns={columns}
          rowHeader="subjectCn"
          noItemsMessage="No certificates match this filter."
        />
      </EuiPanel>
    </>
  )
}
