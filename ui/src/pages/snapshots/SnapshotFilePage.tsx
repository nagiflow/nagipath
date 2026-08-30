import { useEffect, useRef } from 'react'
import { useParams, useSearchParams } from 'react-router-dom'
import { useSnapshotFile } from '../../api/queries/snapshots'
import { PageHeader } from '../../components/shared/PageHeader'
import { PanelHeader } from '../../components/shared/PanelHeader'
import { Button, EmptyPrompt, Loading, Panel } from '../../components/ui'

// Ported from internal/web/templates/file.html against
// GET /api/snapshots/{id}/file/{fileID} (internal/api/snapshots.go), laid out as
// design/'s screen 2s — the far end of every provenance link. The line list below
// mirrors CodeBlock's own .code/.cl/.no markup (components/ui/CodeBlock.tsx) but
// is rendered by hand rather than through that component, because this is
// the one place that needs to scroll a specific line into view on load and
// CodeBlock doesn't expose a ref into individual lines.
export function SnapshotFilePage() {
  const { id = '', fileID = '' } = useParams()
  const [params] = useSearchParams()
  const b = params.get('b')
  const { data, isPending, isError, error } = useSnapshotFile(Number(id), Number(fileID), b ? Number(b) : undefined)
  const highlightRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    highlightRef.current?.scrollIntoView({ block: 'center' })
  }, [data])

  if (isPending) return <Loading label="Loading file…" />
  if (isError) return <EmptyPrompt danger title="Could not load this file" body={error.message} />

  const lines = data.body.split('\n')

  return (
    <>
      <PageHeader
        title={data.file?.path}
        meta={`${data.instance?.displayName} · captured ${data.snapshot?.capturedAt && new Date(data.snapshot.capturedAt).toLocaleString()}`}
      />
      <div className="bd">
        <Panel style={{ flex: 1 }}>
          <PanelHeader
            title={data.hasAnchor ? `Lines ${data.lineStart}–${data.lineEnd}` : 'Whole file'}
            meta={data.hasAnchor ? 'anchored from the rule that fired · read-only capture' : 'read-only capture'}
            actions={<Button small subtle href="/snapshots">Open snapshot</Button>}
          />
          <div className="code">
            {lines.map((line, i) => {
              const lineNo = i + 1
              const highlighted = data.hasAnchor && lineNo >= data.lineStart && lineNo <= data.lineEnd
              return (
                <div
                  key={i}
                  ref={highlighted && lineNo === data.lineStart ? highlightRef : undefined}
                  className={`cl${highlighted ? ' on' : ''}`}
                >
                  <span className="no">{lineNo}</span>
                  <span>{line}</span>
                </div>
              )
            })}
          </div>
        </Panel>
      </div>
    </>
  )
}
