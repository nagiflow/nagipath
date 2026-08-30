import { useEffect, useRef, useState, type ReactNode } from 'react'
import { useNavigate } from 'react-router-dom'
import type { ImportedNode, ImportNodesResponse, TestConnectionResponse } from '../../api/pb/nagipath/api/v1/nodes_pb'
import { useCollectNodes, useImportNodes, useTestNodeConnection } from '../../api/queries/nodes'
import { useCredentials, useDecideHostKey } from '../../api/queries/settings'
import { PageHeader } from '../../components/shared/PageHeader'
import { PanelHeader } from '../../components/shared/PanelHeader'
import { Badge, Button, CallOut, Checkbox, Kv, Label, Panel, Select, Steps } from '../../components/ui'

type TestStatus = 'testing' | 'connected' | 'failed' | 'host_key_pending'

interface RowState {
  status: TestStatus
  result?: TestConnectionResponse
  excluded: boolean
}

const STEPS = ['Paste inventory', 'Test connections & host keys', 'Confirm & import']

// The badge is a fixed-height pill, not a wrapping block, so a long dial
// error (a stack of "dial tcp x: connect: connection refused") gets elided
// here — the full text still reaches the DOM as the badge's title.
function elide(s: string, max = 22): string {
  return s.length > max ? `${s.slice(0, max - 1)}…` : s
}

function ConnectionBadge({ row }: { row?: RowState }) {
  if (!row || row.excluded) return <Badge cls="e">EXCLUDED</Badge>
  switch (row.status) {
    case 'connected': return <Badge cls="v">{`CONNECTED · ${row.result?.latencyMs ?? 0}ms`}</Badge>
    case 'host_key_pending': return <Badge cls="d">BLOCKED · approve key</Badge>
    case 'failed': {
      const error = row.result?.error || 'unknown error'
      return <Badge cls="r" title={error}>{`FAILED · ${elide(error)}`}</Badge>
    }
    default: return <Badge cls="i">TESTING…</Badge>
  }
}

// Its own component so each row gets its own useTestNodeConnection(id) and
// useDecideHostKey() instance — the same reason CollectNowButton on the node
// list page (NodesListPage.tsx) is split out. Fires its test once on mount:
// step 2's whole point is that connectivity is proven before the operator
// leaves this page, not deferred to the next scheduled run.
function ImportTestRow({ node, zebra, onChange }: {
  node: ImportedNode
  zebra: boolean
  onChange: (id: number, state: RowState) => void
}) {
  const id = Number(node.id)
  const test = useTestNodeConnection(id)
  const decide = useDecideHostKey()
  const [open, setOpen] = useState(false)
  const [excluded, setExcluded] = useState(false)
  const started = useRef(false)

  useEffect(() => {
    if (started.current) return
    started.current = true
    test.mutate()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const status: TestStatus = test.data ? (test.data.status as TestStatus) : test.isError ? 'failed' : 'testing'

  useEffect(() => {
    onChange(id, { status, result: test.data, excluded })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [status, test.data, excluded])

  const pendingKey = test.data?.pendingKey
  const changed = !!pendingKey && (test.data?.previousFingerprint ?? '') !== ''
  const zz = zebra ? 'zz' : undefined

  async function decideKey(approve: boolean) {
    if (!pendingKey) return
    await decide.mutateAsync({ id: Number(pendingKey.id), approve })
    if (approve) test.mutate()
    else setExcluded(true)
  }

  return (
    <>
      <tr className={zz}>
        <td className="m">{node.displayName}</td>
        <td className="m mu">{node.address}:{node.sshPort}</td>
        <td className="m mu">{test.data?.osFamily || '—'}</td>
        <td><ConnectionBadge row={{ status, result: test.data, excluded }} /></td>
        <td>
          {!excluded && status === 'host_key_pending' && <Button small onClick={() => setOpen((o) => !o)}>Review</Button>}
          {!excluded && status === 'failed' && <Button small onClick={() => test.mutate()}>Retry</Button>}
        </td>
      </tr>
      {!excluded && status === 'host_key_pending' && open && (
        <tr className={zz}>
          <td className="ex" colSpan={5}>
            <div className="row" style={{ alignItems: 'flex-start' }}>
              <div className="col" style={{ flex: 1, gap: 7 }}>
                <Kv rows={[
                  ...(changed
                    ? ([['recorded', <span key="r">{test.data!.previousFingerprint} · approved</span>]] as [ReactNode, ReactNode][])
                    : []),
                  ['presented', <span key="p">{pendingKey?.fingerprint} · {pendingKey?.algorithm} · seen just now</span>],
                ]} />
                <div style={{ display: 'flex', gap: 6 }}>
                  <Button primary small loading={decide.isPending} onClick={() => decideKey(true)}>
                    {changed ? 'Approve new key & retest' : 'Approve & retest'}
                  </Button>
                  <Button subtle small loading={decide.isPending} onClick={() => decideKey(false)}>Reject &amp; exclude node</Button>
                </div>
              </div>
            </div>
          </td>
        </tr>
      )}
    </>
  )
}

// Ported from internal/web/templates/inventory_import.html (docs/adr/0017,
// Phase 9), laid out as design/'s screen 2q — now a 3-step wizard rather than
// the single paste-and-import form the pre-SPA page was.
//
// Step 1 still writes the node rows immediately (POST /nodes/import,
// internal/api/nodeservice.go) — an added-but-untested row is harmless, the
// same "nothing destructive happens immediately" reasoning the rest of the
// product follows, so there is no separate dry-run parse to maintain.
// Step 2 then tests each added node for real (POST /nodes/{id}/test): the
// same dial the collector opens with, run once here so a bad address or an
// unapproved host key surfaces before the operator walks away. A brand-new
// node has no approved key yet, so its first test always comes back
// host_key_pending — approving inline (reusing Settings / Host keys'
// DecideHostKey) is what lets a retest reach connected. Step 3 is a summary
// and an optional "start collection now" for whatever tested ready; nodes
// left blocked, failed or excluded just sit uncollected, same as they always
// have, reachable later from the Nodes list.
export function ImportNodesPage() {
  const navigate = useNavigate()
  const { data: credentials } = useCredentials('')
  const importNodes = useImportNodes()
  const collectNodes = useCollectNodes()

  const [step, setStep] = useState<1 | 2 | 3>(1)
  const [inventory, setInventory] = useState('')
  const [credentialId, setCredentialId] = useState('')
  const [result, setResult] = useState<ImportNodesResponse>()
  const [rows, setRows] = useState<Record<number, RowState>>({})
  const [startCollection, setStartCollection] = useState(true)

  async function submitStep1() {
    const resp = await importNodes.mutateAsync({ inventory, credentialId: Number(credentialId) })
    setResult(resp)
    if (resp.added.length > 0) {
      setRows({})
      setStep(2)
    }
  }

  const added = result?.added ?? []
  const states = added.map((n) => rows[Number(n.id)])
  const testing = states.filter((r) => !r || r.status === 'testing').length
  const readyIds = added.filter((n) => rows[Number(n.id)]?.status === 'connected' && !rows[Number(n.id)]?.excluded).map((n) => Number(n.id))
  const notReady = added.length - readyIds.length

  async function finish() {
    if (startCollection && readyIds.length > 0) await collectNodes.mutateAsync(readyIds)
    navigate('/nodes')
  }

  return (
    <>
      <PageHeader
        title="Import inventory"
        meta="add hosts from a list or an Ansible inventory"
        actions={<span className="m mus">nagipath never scans a network or CIDR range</span>}
      />
      <Steps steps={STEPS} current={step - 1} />

      {step === 1 && (
        <div className="bd">
          <Panel style={{ maxWidth: 720 }}>
            {result && result.added.length === 0 && result.refused.length > 0 && (
              <div style={{ marginBottom: 10 }}>
                <CallOut title="Every line was refused" color="warning">
                  <ul style={{ margin: '4px 0 0 16px' }}>
                    {result.refused.map((line) => <li key={line}>{line}</li>)}
                  </ul>
                </CallOut>
              </div>
            )}
            <div className="col">
              <div className="col" style={{ gap: 4 }}>
                <Label>Inventory</Label>
                <span className="fld f">
                  <textarea
                    rows={6}
                    value={inventory}
                    onChange={(e) => setInventory(e.target.value)}
                    placeholder={'lb01.example.com\nops@lb02.example.com:2222\n\n[edge]\nweb01 ansible_host=10.90.4.2'}
                  />
                </span>
                <span className="m mus">Paste one host per line or an Ansible INI/YAML inventory.</span>
              </div>

              <div className="col" style={{ gap: 4 }}>
                <Label>Credential</Label>
                <Select
                  options={[
                    { value: '', text: 'Select a stored credential' },
                    ...(credentials?.credentials ?? []).map((cred) => ({
                      value: cred.id.toString(),
                      text: `${cred.name} · ${cred.username} · ${cred.authKind}`,
                    })),
                  ]}
                  value={credentialId}
                  onChange={setCredentialId}
                />
              </div>

              {importNodes.isError && <p className="m" style={{ color: '#a1231c' }}>{importNodes.error.message}</p>}

              <div>
                <Button primary loading={importNodes.isPending} onClick={submitStep1}>
                  Import &amp; continue ›
                </Button>
              </div>
            </div>
          </Panel>
        </div>
      )}

      {step === 2 && result && (
        <div className="bd">
          <Panel style={{ maxWidth: 860 }}>
            <PanelHeader
              title={`${added.length} node${added.length === 1 ? '' : 's'} added`}
              meta="testing each one over SSH with the chosen credential"
              actions={<Button subtle small onClick={() => setStep(1)}>‹ Back to paste</Button>}
            />
            {result.refused.length > 0 && (
              <div style={{ margin: '10px 0' }}>
                <CallOut title={`${result.refused.length} line(s) were skipped while parsing`} color="warning">
                  <ul style={{ margin: '4px 0 0 16px' }}>
                    {result.refused.map((line) => <li key={line}>{line}</li>)}
                  </ul>
                </CallOut>
              </div>
            )}
            <table className="t">
              <thead>
                <tr>
                  <th style={{ width: 160 }}>Node</th>
                  <th style={{ width: 150 }}>Address</th>
                  <th style={{ width: 70 }}>OS</th>
                  <th style={{ width: 260 }}>Connection</th>
                  <th style={{ width: 80 }} />
                </tr>
              </thead>
              <tbody>
                {added.map((n, i) => (
                  <ImportTestRow
                    key={n.id.toString()}
                    node={n}
                    zebra={i % 2 === 1}
                    onChange={(id, state) => setRows((cur) => ({ ...cur, [id]: state }))}
                  />
                ))}
              </tbody>
            </table>
            <div style={{ display: 'flex', alignItems: 'center', gap: 10, padding: '9px 2px 2px' }}>
              <span className="m mus">
                {readyIds.length} connected
                {states.some((r) => r?.status === 'host_key_pending' && !r.excluded) && ` · ${states.filter((r) => r?.status === 'host_key_pending' && !r.excluded).length} blocked on host key`}
                {states.some((r) => r?.status === 'failed' && !r.excluded) && ` · ${states.filter((r) => r?.status === 'failed' && !r.excluded).length} failed`}
                {testing > 0 && ` · ${testing} testing`}
              </span>
              <div style={{ flex: 1 }} />
              <Button subtle small onClick={() => setStep(1)}>‹ Back to paste</Button>
              <Button primary small disabled={testing > 0} onClick={() => setStep(3)}>Continue to confirm ›</Button>
            </div>
          </Panel>
        </div>
      )}

      {step === 3 && result && (
        <div className="bd">
          <Panel style={{ maxWidth: 640 }}>
            <PanelHeader title="Confirm & import" meta={`${added.length} node(s) added in step 1`} />
            <div className="col" style={{ gap: 10 }}>
              <Kv
                labelWidth={72}
                rows={[
                  ['ready', `${readyIds.length} connected — will start collecting now if checked below`],
                  ['not ready', `${notReady} blocked, failed or excluded — reachable later from the Nodes list`],
                ]}
              />
              <Checkbox
                checked={startCollection}
                onChange={() => setStartCollection((v) => !v)}
                label={`Start collection now for the ${readyIds.length} ready node${readyIds.length === 1 ? '' : 's'}`}
              />
              <div style={{ display: 'flex', gap: 8 }}>
                <Button subtle onClick={() => setStep(2)}>‹ Back</Button>
                <Button primary loading={collectNodes.isPending} onClick={finish}>Finish</Button>
              </div>
            </div>
          </Panel>
        </div>
      )}
    </>
  )
}
