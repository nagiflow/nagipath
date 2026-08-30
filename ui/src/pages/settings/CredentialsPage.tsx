import { useState } from 'react'
import { useSession } from '../../api/queries/session'
import { useAddCredential, useCredentials } from '../../api/queries/settings'
import type { CredentialItem } from '../../api/pb/nagipath/api/v1/settings_pb'
import { PanelHeader } from '../../components/shared/PanelHeader'
import {
  Badge, Button, Disclosure, Field, Loading, Panel, PanelFooter, Select, Table,
  type Column,
} from '../../components/ui'
import { SettingsLayout } from './SettingsLayout'

const authKinds = [
  { value: 'private_key', text: 'SSH private key' },
  { value: 'ssh_certificate', text: 'SSH certificate' },
  { value: 'username_password', text: 'Username + password' },
  { value: 'ldap', text: 'LDAP' },
  { value: 'kerberos', text: 'Kerberos' },
  { value: 'cyberark', text: 'CyberArk reference' },
]

const emptyForm = {
  name: '', username: '', authKind: 'private_key', privateKey: '', passphrase: '',
  certificate: '', password: '', externalRef: '',
}

// design/'s State column. Nothing records an auth failure or a missing sudo
// against a credential — collection failures are recorded per node — so the only
// states the data supports are "assigned and exercised", "assigned but never
// exercised", and "assigned to nothing".
function state(c: CredentialItem): { cls: string; text: string } {
  if (c.nodeCount === 0) return { cls: 'i', text: 'UNUSED' }
  if (!c.lastUsed) return { cls: 'i', text: 'NEVER USED' }
  return { cls: 'v', text: 'OK' }
}

// "ssh-ed25519 AAAA…" → "ed25519". Blank for password/reference kinds, which
// have no public key.
function keyType(publicKey: string): string {
  const t = publicKey.split(' ')[0] ?? ''
  return t.replace(/^ssh-/, '')
}

function hhmm(ts: string): string {
  return ts ? new Date(ts).toLocaleString() : 'never'
}

// .t is not table-layout:fixed, so a 50-character fingerprint pushes the State
// column off the panel. design/ elides the middle the same way; the row's
// expansion carries the whole value.
function fp(s: string): string {
  return s.length > 26 ? `${s.slice(0, 18)}…${s.slice(-4)}` : s
}

// A labeled form row — same .lbl/.fld stack SetupPage and PasswordPage use
// for their forms; kept local since no two settings forms share a shape.
function FieldRow({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="col" style={{ gap: 4 }}>
      <span className="lbl">{label}</span>
      {children}
    </div>
  )
}

// Ported from internal/web/templates/credentials.html against
// GET/POST /api/settings/credentials (internal/api/settingsservice.go), laid out
// as design/'s screen 8a: the Type-filtered table, each credential's detail
// expanded under its own row.
// Omitted from that screen: the "Assigned to" cluster-glob column (assignment is
// per node — node.credential_id — so a credential has a node count, not a glob;
// the Fingerprint column takes that slot instead), "Test on 5 nodes"/Reassign/
// Rotate and the whole "Last test" panel (there is no connect+sudo test runner
// and nothing stores its results), the "jump host" and "sudo" detail rows (both
// are node facts — node.bastion_node_id and node.sudo_available — not credential
// ones), and "added … by <user>" (credential.created_by is written but
// credentialCols does not read it back).
export function CredentialsPage() {
  const { data: session } = useSession()
  const isAdmin = session?.user?.role === 'admin'
  const [type, setType] = useState('')
  const [filter, setFilter] = useState('')
  const [selected, setSelected] = useState<bigint | null>(null)
  const [adding, setAdding] = useState(false)
  const { data, isPending, isError, error } = useCredentials(type)
  const add = useAddCredential()
  const [form, setForm] = useState(emptyForm)

  const all = data?.credentials ?? []
  const rows = filter
    ? all.filter((c) => `${c.name} ${c.username}`.toLowerCase().includes(filter.toLowerCase()))
    : all

  const columns: Column<CredentialItem>[] = [
    {
      name: 'Name',
      width: 190,
      render: (c) => <span className="m" style={c.id === selected ? { fontWeight: 500 } : undefined}><Disclosure open={c.id === selected} />{c.name}</span>,
    },
    { name: 'Type', width: 110, render: (c) => <span className="m mu">{c.authKind}</span> },
    { name: 'Username', width: 130, render: (c) => <span className="m mu">{c.username || '—'}</span> },
    {
      name: 'Fingerprint',
      render: (c) => {
        const v = c.fingerprint || c.externalRef
        return <span className="m mu" title={v}>{v ? fp(v) : '—'}</span>
      },
    },
    { name: 'Nodes', width: 80, render: (c) => <span className="m">{c.nodeCount}</span> },
    { name: 'Last used', width: 110, render: (c) => <span className={c.lastUsed ? 'm mu' : 'm mus'}>{hhmm(c.lastUsed)}</span> },
    {
      name: 'State',
      width: 100,
      render: (c) => {
        const st = state(c)
        return <Badge cls={st.cls}>{st.text}</Badge>
      },
    },
  ]

  return (
    <SettingsLayout
      title="Credentials"
      actions={
        <>
          <span className="m mus">stored encrypted with the master key</span>
          {isAdmin && <Button small onClick={() => setAdding(!adding)}>Add credential</Button>}
        </>
      }
    >
      <div className="col">
        {isPending && <Loading label="Loading credentials…" />}
        {isError && <div className="m" style={{ color: '#a1231c' }}>{error.message}</div>}

        {data && (
          <Panel z>
            <PanelHeader
              title="Credentials"
              meta="used by collection over SSH · expand a credential to see what it is and where it is used"
              actions={
                <>
                  <span className="fld" style={{ flex: '0 0 160px' }}>
                    <span className="m mus">filter</span>
                    <input value={filter} onChange={(e) => setFilter(e.target.value)} />
                  </span>
                  <Select
                    options={[{ value: '', text: 'Type: all' },
                      ...(data.authKinds ?? []).map((k) => ({ value: k, text: `Type: ${k}` }))]}
                    value={type}
                    onChange={setType}
                  />
                </>
              }
            />
            <Table
              items={rows}
              columns={columns}
              rowKey={(c) => c.id.toString()}
              rowClassName={(c) => (c.id === selected ? 'hl' : '')}
              onRowClick={(c) => setSelected(c.id === selected ? null : c.id)}
              emptyMessage="No credentials stored yet."
              renderExpanded={(c) => c.id !== selected ? null : (
                <div className="kv m" style={{ display: 'grid', gridTemplateColumns: '100px 1fr', gap: '5px 8px', maxWidth: 620 }}>
                  <span className="mus">type</span><span>{c.authKind}</span>
                  {keyType(c.publicKey) && <><span className="mus">key type</span><span>{keyType(c.publicKey)}</span></>}
                  {c.fingerprint && <><span className="mus">fingerprint</span><span>{c.fingerprint}</span></>}
                  {c.externalRef && <><span className="mus">reference</span><span>{c.externalRef}</span></>}
                  <span className="mus">added</span><span>{c.createdAt ? new Date(c.createdAt).toLocaleString() : '—'}</span>
                  <span className="mus">nodes</span><span>{c.nodeCount} assigned · last used {hhmm(c.lastUsed)}</span>
                  <span className="mus">secret</span><span className="mus">write-only · never displayed</span>
                </div>
              )}
            />
            <PanelFooter>
              <span className="m mus">expand a credential to see what it is and where it is used</span>
              <div style={{ flex: 1 }} />
              <span className="m mus">{rows.length} of {all.length}</span>
            </PanelFooter>
          </Panel>
        )}

        {adding && isAdmin && (
          <Panel style={{ maxWidth: 420 }}>
            <PanelHeader title="Add a credential" meta="the secret is sealed with the master key on save" />
            <div className="col" style={{ marginTop: 10 }}>
              <FieldRow label="Name">
                <Field value={form.name} onChange={(v) => setForm({ ...form, name: v })} grow />
              </FieldRow>
              <FieldRow label="Type">
                <Select options={authKinds} value={form.authKind} onChange={(v) => setForm({ ...form, authKind: v })} />
              </FieldRow>
              <FieldRow label="Username">
                <Field value={form.username} onChange={(v) => setForm({ ...form, username: v })} grow />
              </FieldRow>

              {(form.authKind === 'private_key' || form.authKind === 'ssh_certificate') && (
                <FieldRow label="Private key">
                  <span className="fld f">
                    <textarea rows={6} value={form.privateKey} onChange={(e) => setForm({ ...form, privateKey: e.target.value })} />
                  </span>
                </FieldRow>
              )}
              {form.authKind === 'ssh_certificate' && (
                <FieldRow label="Certificate">
                  <span className="fld f">
                    <textarea rows={4} value={form.certificate} onChange={(e) => setForm({ ...form, certificate: e.target.value })} />
                  </span>
                </FieldRow>
              )}
              {(form.authKind === 'private_key' || form.authKind === 'ssh_certificate') && (
                <FieldRow label="Passphrase (optional)">
                  <Field type="password" value={form.passphrase} onChange={(v) => setForm({ ...form, passphrase: v })} grow />
                </FieldRow>
              )}
              {(form.authKind === 'username_password' || form.authKind === 'ldap' || form.authKind === 'kerberos') && (
                <FieldRow label="Password">
                  <Field type="password" value={form.password} onChange={(v) => setForm({ ...form, password: v })} grow />
                </FieldRow>
              )}
              {(form.authKind === 'ldap' || form.authKind === 'kerberos' || form.authKind === 'cyberark') && (
                <FieldRow label="External reference (optional)">
                  <Field value={form.externalRef} onChange={(v) => setForm({ ...form, externalRef: v })} grow />
                </FieldRow>
              )}

              {add.isError && <div className="m" style={{ color: '#a1231c' }}>{add.error.message}</div>}
              <div className="row">
                <Button primary loading={add.isPending}
                  onClick={() => add.mutate(form, { onSuccess: () => { setForm(emptyForm); setAdding(false) } })}>
                  Store credential
                </Button>
                <Button subtle onClick={() => setAdding(false)}>Cancel</Button>
              </div>
            </div>
          </Panel>
        )}
      </div>
    </SettingsLayout>
  )
}
