import { EuiBasicTable, EuiLoadingChart, EuiPageTemplate, EuiPanel, EuiSpacer, EuiText, EuiTitle } from '@elastic/eui'
import { useParams } from 'react-router-dom'
import { useCertificate } from '../../api/queries/certificates'
import type { CertBinding } from '../../api/pb/nagipath/api/v1/certificates_pb'

// Ported from internal/web/templates/certificate.html against
// GET /api/ui/certificates/{id} (internal/api/certificates.go).
export function CertificateDetailPage() {
  const { id = '' } = useParams()
  const { data, isPending, isError, error } = useCertificate(Number(id), 'overview')

  if (isPending) return <EuiPageTemplate.EmptyPrompt icon={<EuiLoadingChart size="xl" />} title={<h2>Loading certificate…</h2>} />
  if (isError) return <EuiPageTemplate.EmptyPrompt iconType="alert" color="danger" title={<h2>Could not load this certificate</h2>} body={<p>{error.message}</p>} />

  const cert = data.cert

  return (
    <>
      <EuiTitle size="m"><h1>{cert?.subjectCn || '(no CN)'}</h1></EuiTitle>
      <EuiSpacer size="s" />
      <EuiText size="s" color="subdued">{cert?.issuerDn} · expires {cert?.notAfter && new Date(cert.notAfter).toLocaleDateString()}</EuiText>
      <EuiSpacer />

      <EuiPanel>
        <EuiText size="s">
          <p>Fingerprint: {cert?.fingerprint}</p>
          <p>Serial: {cert?.serial}</p>
          <p>Key: {cert?.keyAlgorithm} {cert?.keyBits}</p>
          <p>SANs: {cert?.sans.join(', ') || 'none'}</p>
        </EuiText>
      </EuiPanel>
      <EuiSpacer />

      <EuiPanel>
        <EuiTitle size="xs"><h2>Bindings ({data.bindingCount})</h2></EuiTitle>
        <EuiSpacer size="s" />
        <EuiBasicTable<CertBinding>
          items={data.bindings}
          columns={[
            { field: 'instance', name: 'Instance' },
            { field: 'node', name: 'Node' },
            { field: 'clusterName', name: 'Cluster' },
            { field: 'siteNames', name: 'Sites' },
            { field: 'port', name: 'Port' },
          ]}
          noItemsMessage="No bindings."
        />
      </EuiPanel>
    </>
  )
}
