import { useState } from 'react'
import type { Meta, StoryObj } from '@storybook/react-vite'
import * as Ui from '.'

// One story per primitive, each living in its own file (Panel.tsx, Button.tsx,
// …) next to its own CSS — this app's literal classes, not a component
// library themed to look like it. Button, Badge and Table have their own
// story files; everything else that doesn't warrant one lives here.
//
// Imported as a namespace (`Ui.Panel`, `Ui.Button`, …) rather than named
// imports: each primitive's own story is exported under its bare name
// (`export const Panel`, `export const Button`), which would otherwise
// collide with a same-named import in the same module.
const meta: Meta = {
  title: 'Components/Primitives',
  parameters: {
    docs: {
      description: {
        component: 'The full primitive set. If a page needs something not here, add a new file next to these — never straight into the page.',
      },
    },
  },
}
export default meta

type Story = StoryObj

export const PageHeader: Story = {
  name: 'PageHeader (.ptitle)',
  render: () => (
    <Ui.PageHeader
      title="app-iad3-17"
      badge={<span className="bg d">DRIFT</span>}
      meta="10.4.12.17:22 · nginx 1.24.0"
      actions={<><Ui.Button small subtle>Collect now</Ui.Button><Ui.Button small primary>Open drift</Ui.Button></>}
    />
  ),
}

export const Steps: Story = {
  name: 'Steps (.stp)',
  render: () => <Ui.Steps steps={['Paste inventory', 'Test connections & host keys', 'Confirm & import']} current={1} />,
}

export const PanelHeader: Story = {
  name: 'PanelHeader (.phd)',
  render: () => (
    <Ui.Panel z>
      <Ui.PanelHeader title="Needs attention" meta="4 items" actions={<Ui.Button small subtle>Severity: all</Ui.Button>} />
      <div style={{ padding: 12 }} className="m mu">panel body</div>
    </Ui.Panel>
  ),
}

export const Panel: Story = {
  name: 'Panel (.pnl / .pnl.z)',
  render: () => (
    <div className="row">
      <Ui.Panel style={{ flex: 1 }}>
        <span className="lbl">Plain panel</span>
        <div className="m mu">Padded, used for forms and summaries.</div>
      </Ui.Panel>
      <Ui.Panel z style={{ flex: 1, height: 90 }}>
        <Ui.PanelHeader title="Clipping panel" />
        <div style={{ padding: 12 }} className="m mu">`.z` clips its own content so a table scrolls inside it.</div>
      </Ui.Panel>
    </div>
  ),
}

function QueryBarDemo() {
  const [q, setQ] = useState('')
  return (
    <Ui.QueryBar>
      <Ui.Field grow placeholder="hostname, listener or certificate" value={q} onChange={setQ} />
      <Ui.Select value="all" onChange={() => {}} options={[{ value: 'all', text: 'Cluster: all' }]} />
      <Ui.Select value="risk" onChange={() => {}} options={[{ value: 'risk', text: 'Sort: risk' }]} />
      <Ui.Button small subtle>Reset</Ui.Button>
    </Ui.QueryBar>
  )
}

export const QueryBar: Story = { name: 'QueryBar (.qbar)', render: () => <QueryBarDemo /> }

export const StatRow: Story = {
  name: 'StatRow (.stat)',
  render: () => (
    <Ui.StatRow
      stats={[
        { label: 'Nodes', value: 428, sub: '4 vendors' },
        { label: 'Processes', value: 1129, sub: '1041 fresh < 6h' },
        { label: 'Certs ≤ 30d', value: 41, sub: '63 bindings', tone: 'warning' },
        { label: 'Unreachable', value: 2, sub: 'since 04:12', tone: 'danger' },
      ]}
    />
  ),
}

export const Kv: Story = {
  name: 'Kv (.kv)',
  render: () => (
    <Ui.Panel>
      <Ui.Kv
        rows={[
          ['subject', 'checkout.example.com'],
          ['issuer', 'CN=Example Internal CA G2'],
          ['expires', '2026-09-08 · 9 days'],
          ['bindings', 12],
          ['sans', 'checkout.example.com, checkout-int.example.com'],
        ]}
      />
    </Ui.Panel>
  ),
}

export const Facets: Story = {
  name: 'Facet (.fct)',
  render: () => (
    <Ui.Panel style={{ width: 260 }}>
      <span className="lbl">Vendor</span>
      <Ui.Facet label="nginx" count={1720} />
      <Ui.Facet label="haproxy" count={96} />
      <Ui.Facet label="apache" count={26} />
    </Ui.Panel>
  ),
}

function InputsDemo() {
  const [text, setText] = useState('checkout.example.com')
  const [sel, setSel] = useState('30d')
  const [checked, setChecked] = useState(true)
  const [radio, setRadio] = useState('golden')
  return (
    <div className="col" style={{ maxWidth: 420 }}>
      <Ui.Field placeholder="hostname" value={text} onChange={setText} />
      <Ui.Field placeholder="disabled" value="" onChange={() => {}} disabled />
      <Ui.Select
        value={sel}
        onChange={setSel}
        options={[{ value: '30d', text: 'Expires: ≤ 30 days' }, { value: '90d', text: 'Expires: ≤ 90 days' }, { value: 'all', text: 'Expires: any' }]}
      />
      <Ui.Checkbox id="cb" checked={checked} onChange={() => setChecked(!checked)} label="Include CA certificates" />
      <div className="row">
        <Ui.Radio name="baseline" checked={radio === 'golden'} onChange={() => setRadio('golden')} label="Golden peer" />
        <Ui.Radio name="baseline" checked={radio === 'previous'} onChange={() => setRadio('previous')} label="Previous snapshot" />
      </div>
    </div>
  )
}

export const Inputs: Story = { render: () => <InputsDemo /> }

function TabsDemo() {
  const [tab, setTab] = useState('routes')
  return (
    <Ui.Tabs
      selected={tab}
      onSelect={setTab}
      tabs={[
        { id: 'overview', label: 'Overview' },
        { id: 'sites', label: 'Sites · 31' },
        { id: 'routes', label: 'Routes · 214' },
        { id: 'upstreams', label: 'Upstreams · 8' },
        { id: 'files', label: 'Config files · 14' },
      ]}
    />
  )
}

export const Tabs: Story = { name: 'Tabs (.tabs)', render: () => <TabsDemo /> }

export const CodeBlock: Story = {
  name: 'CodeBlock (.code)',
  render: () => (
    <Ui.CodeBlock
      startLine={9}
      highlight={(n) => n === 10}
      lines={[
        '    location /api/v2/ {',
        '        proxy_pass http://checkout_api_canary;',
        '        proxy_set_header X-Request-Id $request_id;',
        '    }',
      ]}
    />
  ),
}

export const Text: Story = {
  name: 'Label / Mono / Chip',
  render: () => (
    <div className="col">
      <Ui.Label>Collection coverage</Ui.Label>
      <div className="row">
        <Ui.Mono>10.4.12.17:22</Ui.Mono>
        <Ui.Mono subdued>nginx 1.24.0</Ui.Mono>
        <Ui.Mono subtle>never captured</Ui.Mono>
      </div>
      <div className="row">
        <Ui.Chip><b>cluster</b> app-iad3 <span className="x">×</span></Ui.Chip>
        <Ui.Chip><b>vendor</b> nginx <span className="x">×</span></Ui.Chip>
      </div>
    </div>
  ),
}

export const Bar: Story = {
  name: 'Bar (.bar)',
  render: () => (
    <div className="col" style={{ maxWidth: 320 }}>
      <div className="row" style={{ alignItems: 'center' }}><span className="m mu" style={{ width: 90 }}>app-iad3</span><Ui.Bar pct={96} /><span className="m mu">96%</span></div>
      <div className="row" style={{ alignItems: 'center' }}><span className="m mu" style={{ width: 90 }}>legacy</span><Ui.Bar pct={58} /><span className="m mu">58%</span></div>
    </div>
  ),
}

export const States: Story = {
  name: 'Loading / EmptyPrompt / CallOut',
  render: () => (
    <div className="col">
      <Ui.Panel><Ui.Loading label="Loading dashboard…" /></Ui.Panel>
      <Ui.EmptyPrompt title="No drift in this cluster" body="Every member matches its golden peer as of the last diff job." />
      <Ui.EmptyPrompt danger title="Could not load the dashboard" body="index is locked by a prune job" />
      <Ui.CallOut title="Retention floor">Audit events cannot be set below 365 days by any role.</Ui.CallOut>
      <Ui.CallOut color="danger" title="Host key changed">Nothing is collected from this node until the new key is approved.</Ui.CallOut>
      <Ui.CallOut color="success" title="Probe verified 2 hops">Both hops moved from INFERRED to VERIFIED.</Ui.CallOut>
    </div>
  ),
}

export const Accordion: Story = {
  name: 'Accordion',
  render: () => (
    <Ui.Accordion title="Entry candidates · 2">
      <Ui.Panel>
        <Ui.Kv rows={[['selected', 'nginx (:443) on edge-iad3-01'], ['collapsed', 'nginx (:443) on edge-iad3-02 — identical config']]} />
      </Ui.Panel>
    </Ui.Accordion>
  ),
}

function ModalsDemo() {
  const [which, setWhich] = useState<'none' | 'plain' | 'confirm'>('confirm')
  return (
    <div className="row">
      <Ui.Button onClick={() => setWhich('plain')}>Open modal</Ui.Button>
      <Ui.Button danger onClick={() => setWhich('confirm')}>Open confirm</Ui.Button>
      {which === 'plain' && (
        <Ui.Modal title="New API key" onClose={() => setWhich('none')} footer={<><Ui.Button subtle onClick={() => setWhich('none')}>Cancel</Ui.Button><Ui.Button primary>Create</Ui.Button></>}>
          <Ui.Field placeholder="name" value="ci-deploy" onChange={() => {}} />
        </Ui.Modal>
      )}
      {which === 'confirm' && (
        <Ui.ConfirmModal
          danger
          title="Delete node app-iad3-17?"
          body="Snapshots and drift findings for this node are deleted with it. This cannot be undone."
          confirmLabel="Delete node"
          onCancel={() => setWhich('none')}
          onConfirm={() => setWhich('none')}
        />
      )}
    </div>
  )
}

export const Modals: Story = { render: () => <ModalsDemo /> }

export const PanelFooter: Story = {
  name: 'PanelFooter',
  render: () => (
    <Ui.Panel z style={{ height: 120 }}>
      <Ui.PanelHeader title="Snapshots" />
      <div style={{ flex: 1 }} />
      <Ui.PanelFooter>
        <span className="m mus">1–5 of 18,422</span>
        <div style={{ flex: 1 }} />
        <Ui.Button small subtle>Load more</Ui.Button>
      </Ui.PanelFooter>
    </Ui.Panel>
  ),
}
