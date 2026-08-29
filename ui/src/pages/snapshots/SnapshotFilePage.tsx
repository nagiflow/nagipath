import { EuiLoadingChart, EuiPageTemplate, EuiPanel, EuiSpacer, EuiText, EuiTitle } from '@elastic/eui'
import { useEffect, useRef } from 'react'
import { useParams, useSearchParams } from 'react-router-dom'
import { useSnapshotFile } from '../../api/queries/snapshots'

// Ported from internal/web/templates/file.html against
// GET /api/ui/snapshots/{id}/file/{fileID} (internal/api/snapshots.go) —
// the far end of every provenance link in the product.
export function SnapshotFilePage() {
  const { id = '', fileID = '' } = useParams()
  const [params] = useSearchParams()
  const b = params.get('b')
  const { data, isPending, isError, error } = useSnapshotFile(Number(id), Number(fileID), b ? Number(b) : undefined)
  const highlightRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    highlightRef.current?.scrollIntoView({ block: 'center' })
  }, [data])

  if (isPending) return <EuiPageTemplate.EmptyPrompt icon={<EuiLoadingChart size="xl" />} title={<h2>Loading file…</h2>} />
  if (isError) return <EuiPageTemplate.EmptyPrompt iconType="alert" color="danger" title={<h2>Could not load this file</h2>} body={<p>{error.message}</p>} />

  const lines = data.body.split('\n')

  return (
    <>
      <EuiTitle size="m"><h1>{data.file?.path}</h1></EuiTitle>
      <EuiSpacer size="s" />
      <EuiText size="s" color="subdued">
        {data.instance?.displayName} · captured {data.snapshot?.capturedAt && new Date(data.snapshot.capturedAt).toLocaleString()}
      </EuiText>
      <EuiSpacer />

      <EuiPanel>
        <pre style={{ margin: 0, fontSize: 12, overflowX: 'auto' }}>
          {lines.map((line, i) => {
            const lineNo = i + 1
            const highlighted = data.hasAnchor && lineNo >= data.lineStart && lineNo <= data.lineEnd
            return (
              <div
                key={i}
                ref={highlighted && lineNo === data.lineStart ? highlightRef : undefined}
                style={{ background: highlighted ? '#fff8e1' : undefined, whiteSpace: 'pre-wrap' }}
              >
                <span style={{ color: '#98a2b3', userSelect: 'none', display: 'inline-block', width: 40 }}>{lineNo}</span>
                {line}
              </div>
            )
          })}
        </pre>
      </EuiPanel>
    </>
  )
}
