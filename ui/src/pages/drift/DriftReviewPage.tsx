import { useState } from 'react'
import { useParams } from 'react-router-dom'
import { useDriftReview, useIgnoreFinding } from '../../api/queries/drift'
import { useSession } from '../../api/queries/session'
import type { DriftFinding } from '../../api/pb/nagipath/api/v1/nodes_pb'
import { PageHeader } from '../../components/shared/PageHeader'
import { PanelHeader } from '../../components/shared/PanelHeader'
import {
  Badge, Bar, Button, Checkbox, EmptyPrompt, Loading, Modal, Panel,
  PanelFooter,
} from '../../components/ui'

const changeBadge: Record<string, { cls: string; text: string }> = {
  changed: { cls: 'd', text: 'MODIFIED' },
  added: { cls: 'n', text: 'ONLY HERE' },
  removed: { cls: 'd', text: 'MISSING HERE' },
  reordered: { cls: 'i', text: 'REORDERED' },
}

function lineCount(s: string): number {
  return s ? s.split('\n').length : 0
}

// design/'s "+6 −3" per file. An object diff has no hunks, so the counts are the
// lines of the object's own text on each side.
function plusMinus(f: DriftFinding): { add: number; del: number } {
  return { add: lineCount(f.subjectText), del: lineCount(f.baselineText) }
}

// One card of design/'s screen 6a: the object's header strip and, when open, the
// baseline text beside this node's text.
function ObjectCard({ f, open, viewed, onToggle, onViewed, onIgnore }: {
  f: DriftFinding
  open: boolean
  viewed: boolean
  onToggle: () => void
  onViewed: () => void
  onIgnore?: () => void
}) {
  const { add, del } = plusMinus(f)
  const badge = changeBadge[f.change] ?? { cls: 'd', text: f.change.toUpperCase() }
  const baseline = f.baselineText.split('\n')
  const subject = f.subjectText.split('\n')
  const rows = Math.max(baseline.length, subject.length)

  return (
    <div style={{ border: '1px solid #d3dae6', borderRadius: 5, overflow: 'hidden', flex: 'none', background: '#fff' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 9, padding: '6px 9px', background: '#f7f8fc', borderBottom: open ? '1px solid #d3dae6' : undefined }}>
        <span className="m mus" onClick={onToggle} style={{ cursor: 'pointer' }}>{open ? '▾' : '▸'}</span>
        <span className="m" style={{ fontWeight: 600, cursor: 'pointer' }} onClick={onToggle}>{f.naturalKey}</span>
        <span className="m" style={{ color: '#00726b' }}>+{add}</span>
        <span className="m" style={{ color: '#a1231c' }}>−{del}</span>
        <Badge cls={badge.cls}>{badge.text}</Badge>
        {f.field && <span className="m mus">{f.field}</span>}
        <div style={{ flex: 1 }} />
        {onIgnore && <Button small subtle onClick={onIgnore}>Ignore</Button>}
        {f.provenance?.link && <Button small subtle href={f.provenance.link}>Open file</Button>}
        <span className="fct" style={{ gap: 5 }}>
          <Checkbox id={`viewed-${f.id}`} checked={viewed} onChange={onViewed} label={<span className="m mus">Viewed</span>} />
        </span>
      </div>
      {open && (f.baselineText || f.subjectText) && (
        <div className="code" style={{ border: 0, borderRadius: 0, padding: 0, background: '#fff' }}>
          {Array.from({ length: rows }, (_, i) => {
            const b = baseline[i]
            const s = subject[i]
            const differs = b !== s
            return (
              <div style={{ display: 'flex' }} key={i}>
                <div style={{ flex: 1, minWidth: 0, borderRight: '1px solid #edf0f5' }}>
                  <div className="cl" style={b === undefined ? { background: '#f7f8fc' } : differs ? { background: '#fceeed', boxShadow: 'inset 2px 0 0 #a1231c' } : undefined}>
                    <span className="no">{b === undefined ? '' : i + 1}</span>
                    <span>{b ?? ''}</span>
                  </div>
                </div>
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div className="cl" style={s === undefined ? { background: '#f7f8fc' } : differs ? { background: '#e6f2f1', boxShadow: 'inset 2px 0 0 #00726b' } : undefined}>
                    <span className="no">{s === undefined ? '' : i + 1}</span>
                    <span>{s ?? ''}</span>
                  </div>
                </div>
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}

// Ported from internal/web/templates/drift_review.html against
// GET /api/drift/review/{instanceID} (internal/api/driftservice.go), laid out as
// design/'s screen 6a: the Viewed progress bar, the 252px changed-objects rail
// grouped by kind, and one expandable split-diff card per divergent object.
//
// design/ describes this screen as a GitHub-PR-style *file* diff — raw text from
// two snapshots, no parsing. nagipath's drift engine diffs at the *object* level
// instead (store.diffObjects compares parsed routes, upstreams, sites, listeners
// and rules by natural key), which is a deliberate difference in what is being
// compared, not styling: an object diff survives a config reformat and names the
// thing that changed. So each divergent object stands in for one of design/'s
// files.
// Omitted from that screen: "Export patch"/"Copy diff" (there is no patch — the
// comparison is over parsed objects, not file text), the "View: split"/"Context:
// 3 lines"/"Whitespace: ignored" selects (an object's text is the whole unit, so
// there is no context to widen and nothing to unify or ignore), the "Other nodes
// off baseline" panel (GetDriftReview answers for one instance; the queue on
// /drift is the fleet view), the "26 files identical to baseline" line (no
// object-compared total is recorded — only the findings are), and "Mark node
// reviewed"/"Ignore rule from selection" (nothing stores per-node review state,
// and an ignore rule is written per object kind + pattern, which is the per-card
// Ignore button).
// "Viewed" is local-only — ponytail: add server-side persistence if reviewers
// want it to survive a reload.
export function DriftReviewPage() {
  const { instanceID = '' } = useParams()
  const { data: session } = useSession()
  const { data, isPending, isError, error } = useDriftReview(Number(instanceID))
  const ignore = useIgnoreFinding()
  const [viewed, setViewed] = useState<Set<string>>(new Set())
  const [open, setOpen] = useState<Set<string> | null>(null)
  const [ignoring, setIgnoring] = useState<DriftFinding | null>(null)
  const [reason, setReason] = useState('')

  if (isPending) return <Loading label="Loading…" />
  if (isError) return <EmptyPrompt danger title="Could not load this review" body={error.message} />

  const groups = data.objectGroups
  const findings = groups.flatMap((g) => g.findings)
  const isAdmin = session?.user?.role === 'admin'
  // Default: everything open, so the review reads top to bottom on arrival.
  const isOpen = (id: string) => (open === null ? true : open.has(id))
  const toggleOpen = (id: string) => setOpen((cur) => {
    const next = new Set(cur ?? findings.map((f) => String(f.id)))
    if (next.has(id)) next.delete(id); else next.add(id)
    return next
  })
  const toggleViewed = (id: string) => setViewed((v) => {
    const next = new Set(v)
    if (next.has(id)) next.delete(id); else next.add(id)
    return next
  })

  const totals = findings.reduce((acc, f) => {
    const { add, del } = plusMinus(f)
    return { add: acc.add + add, del: acc.del + del }
  }, { add: 0, del: 0 })
  const pct = findings.length > 0 ? Math.round((viewed.size / findings.length) * 100) : 0
  const openCount = findings.filter((f) => isOpen(String(f.id))).length

  const submitIgnore = () => {
    if (!ignoring) return
    ignore.mutate(
      { cluster: Number(data.clusterId), object_kind: ignoring.objectKind, field: ignoring.field, pattern: ignoring.naturalKey, reason },
      { onSuccess: () => { setIgnoring(null); setReason('') } },
    )
  }

  return (
    <>
      <PageHeader
        title={data.instanceDisplayName}
        badge={<Badge cls="d">{data.objectCount} OBJECT{data.objectCount === 1 ? '' : 'S'} CHANGED</Badge>}
        meta={`${data.nodeDisplayName} · vs ${data.baselineLabel} · diffed ${data.subjectTime ? new Date(data.subjectTime).toLocaleString() : '—'}`}
        actions={data.nextInstanceId > 0n && <Button small href={`/drift/review/${data.nextInstanceId}`}>Next node ›</Button>}
      />

      {findings.length > 0 && (
        <div className="qbar">
          <span className="lbl">Viewed</span>
          <span style={{ width: 120, flex: 'none' }}><Bar pct={pct} /></span>
          <span className="m mus">{viewed.size} of {findings.length} objects</span>
          <span className="m" style={{ color: '#00726b' }}>+{totals.add}</span>
          <span className="m" style={{ color: '#a1231c' }}>−{totals.del}</span>
          <div style={{ flex: 1 }} />
          <Button small subtle onClick={() => setOpen(new Set())}>Collapse all</Button>
          <Button small subtle onClick={() => setViewed(new Set(findings.map((f) => String(f.id))))}>Mark all viewed</Button>
        </div>
      )}

      <div className="bd" style={{ flexDirection: 'row' }}>
        {findings.length === 0 ? (
          // The .bd here is a row, so a bare panel would stretch to full height
          // and content width. In a .col it takes the width and its own height.
          <div className="col" style={{ flex: 1 }}>
            <EmptyPrompt title="Nothing diverges" body={`Every parsed object on ${data.instanceDisplayName} matches ${data.baselineLabel}.`} />
          </div>
        ) : (
          <>
            <div className="col" style={{ flex: '0 0 252px', minWidth: 0 }}>
              <Panel z style={{ flex: 1, minHeight: 0 }}>
                <PanelHeader title="Changed objects" meta={`${findings.length} in ${groups.length} kind${groups.length === 1 ? '' : 's'}`} />
                <div style={{ padding: '5px 7px', display: 'flex', flexDirection: 'column', gap: 1, flex: 1, minHeight: 0, overflow: 'auto' }}>
                  {groups.map((g) => (
                    <div key={g.kind}>
                      <div className="lbl" style={{ padding: '6px 4px 2px' }}>{g.kind}</div>
                      {g.findings.map((f) => {
                        const seen = viewed.has(String(f.id))
                        const { add, del } = plusMinus(f)
                        return (
                          <div
                            key={f.id.toString()}
                            onClick={() => toggleOpen(String(f.id))}
                            style={{
                              display: 'flex', alignItems: 'center', gap: 6, padding: '3px 6px', borderRadius: 3,
                              cursor: 'pointer', opacity: seen ? 0.5 : 1,
                              background: isOpen(String(f.id)) ? '#0077cc' : undefined,
                              color: isOpen(String(f.id)) ? '#fff' : undefined,
                            }}
                          >
                            <span className="m" style={{ flex: 1, minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                              {f.naturalKey}
                            </span>
                            <span className={isOpen(String(f.id)) ? 'm' : 'm mus'} style={{ fontSize: 10, opacity: isOpen(String(f.id)) ? 0.8 : undefined }}>
                              {seen && '✓ '}+{add} −{del}
                            </span>
                          </div>
                        )
                      })}
                    </div>
                  ))}
                  {data.ignoredCount > 0 && (
                    <>
                      <div className="lbl" style={{ padding: '9px 4px 2px' }}>not shown</div>
                      <div className="m mus" style={{ padding: '2px 6px' }}>{data.ignoredCount} object(s) ignored by rule</div>
                    </>
                  )}
                </div>
              </Panel>
            </div>

            <Panel z style={{ flex: 1, minWidth: 0 }}>
              <PanelHeader title="Object diff" meta={`${data.baselineLabel} left · this node right`} />
              <div style={{ flex: 1, minHeight: 0, overflow: 'auto', padding: '9px 11px', display: 'flex', flexDirection: 'column', gap: 9 }}>
                {findings.map((f) => (
                  <ObjectCard
                    key={f.id.toString()}
                    f={f}
                    open={isOpen(String(f.id))}
                    viewed={viewed.has(String(f.id))}
                    onToggle={() => toggleOpen(String(f.id))}
                    onViewed={() => toggleViewed(String(f.id))}
                    onIgnore={isAdmin && Number(data.clusterId) > 0 ? () => { setIgnoring(f); setReason('') } : undefined}
                  />
                ))}
              </div>
              <PanelFooter>
                <span className="m mus">{openCount} expanded · {findings.length - openCount} collapsed</span>
              </PanelFooter>
            </Panel>
          </>
        )}
      </div>

      {ignoring && (
        <Modal
          title="Ignore this object"
          onClose={() => setIgnoring(null)}
          footer={
            <>
              <Button subtle onClick={() => setIgnoring(null)}>Cancel</Button>
              <Button primary loading={ignore.isPending} onClick={submitIgnore}>Ignore on cluster</Button>
            </>
          }
        >
          <div className="m mu">
            Every <span className="m">{ignoring.objectKind}</span> matching{' '}
            <span className="m" style={{ fontWeight: 500 }}>{ignoring.naturalKey}</span>
            {ignoring.field && <> on field <span className="m">{ignoring.field}</span></>} stops being reported as drift
            for this cluster, on every member, until the rule is removed.
          </div>
          <div style={{ height: 10 }} />
          <span className="fld f">
            <span className="m mus">reason</span>
            <input value={reason} onChange={(e) => setReason(e.target.value)} placeholder="why this difference is expected" />
          </span>
          {ignore.isError && <div className="m" style={{ color: '#a1231c', marginTop: 8 }}>{ignore.error.message}</div>}
        </Modal>
      )}
    </>
  )
}
