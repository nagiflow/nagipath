import { EuiButton, EuiFlexGroup, EuiFlexItem, EuiLoadingChart, EuiPageTemplate, EuiPanel, EuiSpacer, EuiText, EuiTitle } from '@elastic/eui'
import { useParams } from 'react-router-dom'
import { useDriftReview } from '../../api/queries/drift'

// Ported from internal/web/templates/drift_review.html against
// GET /api/ui/drift/review/{instanceID} (internal/api/drift.go).
export function DriftReviewPage() {
  const { instanceID = '' } = useParams()
  const { data, isPending, isError, error } = useDriftReview(Number(instanceID))

  if (isPending) return <EuiPageTemplate.EmptyPrompt icon={<EuiLoadingChart size="xl" />} title={<h2>Loading…</h2>} />
  if (isError) return <EuiPageTemplate.EmptyPrompt iconType="alert" color="danger" title={<h2>Could not load this review</h2>} body={<p>{error.message}</p>} />

  return (
    <>
      <EuiFlexGroup alignItems="center" justifyContent="spaceBetween">
        <EuiFlexItem grow={false}>
          <EuiTitle size="m"><h1>{data.instanceDisplayName}</h1></EuiTitle>
          <EuiText size="s" color="subdued">
            {data.nodeDisplayName} · vs {data.baselineLabel} · {data.objectCount} divergent object(s) · {data.ignoredCount} ignored
          </EuiText>
        </EuiFlexItem>
        {data.nextInstanceId > 0n && (
          <EuiFlexItem grow={false}>
            <EuiButton size="s" href={`/drift/review/${data.nextInstanceId}`}>Next divergent instance →</EuiButton>
          </EuiFlexItem>
        )}
      </EuiFlexGroup>
      <EuiSpacer />

      {data.objectGroups.length === 0 ? (
        <EuiText size="s" color="subdued">No divergent objects.</EuiText>
      ) : (
        data.objectGroups.map((g) => (
          <EuiPanel key={g.kind} style={{ marginBottom: 16 }}>
            <EuiTitle size="xs"><h2>{g.kind}</h2></EuiTitle>
            <EuiSpacer size="s" />
            {g.findings.map((f) => (
              <div key={f.id.toString()} style={{ padding: '8px 0', borderBottom: '1px solid #edf0f5' }}>
                <EuiText size="s">
                  <strong>{f.naturalKey}</strong> · {f.change} · {f.field}
                  {f.provenance?.link && (
                    <>
                      {' · '}
                      <a href={f.provenance.link} title={`jump to file · ${f.provenance.path}`}>
                        {f.provenance.path.split('/').slice(-2).join('/')} ↗
                      </a>
                    </>
                  )}
                </EuiText>
                {(f.baselineText || f.subjectText) && (
                  <div className="drift-diff" style={{ display: 'flex', gap: 12, marginTop: 4, fontFamily: 'monospace', fontSize: 12 }}>
                    <div style={{ flex: 1, background: '#fceeed', padding: 6 }}>{f.baselineText}</div>
                    <div style={{ flex: 1, background: '#e6f2f1', padding: 6 }}>{f.subjectText}</div>
                  </div>
                )}
              </div>
            ))}
          </EuiPanel>
        ))
      )}
    </>
  )
}
