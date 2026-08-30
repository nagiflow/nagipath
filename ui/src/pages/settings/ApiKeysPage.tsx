import { useState } from 'react'
import { useSession } from '../../api/queries/session'
import { useAPIKeys, useCreateAPIKey, useRevokeAPIKey } from '../../api/queries/settings'
import type { ApiKeyItem } from '../../api/pb/nagipath/api/v1/settings_pb'
import { PanelHeader } from '../../components/shared/PanelHeader'
import {
  Badge, Button, Disclosure, Field, Loading, Panel, PanelFooter, Select,
  Table, type Column,
} from '../../components/ui'
import { SettingsLayout } from './SettingsLayout'

function days(until: string): number {
  return Math.floor((new Date(until).getTime() - Date.now()) / 86_400_000)
}

// The state a key is actually in, which is not one column in the table: revoked
// beats expired, and a key with no expiry never expires.
function state(k: ApiKeyItem): { cls: string; text: string } {
  if (k.revokedAt) return { cls: 'r', text: 'REVOKED' }
  if (!k.expiresAt) return { cls: 'd', text: 'NO EXPIRY' }
  const d = days(k.expiresAt)
  if (d < 0) return { cls: 'r', text: 'EXPIRED' }
  if (d <= 30) return { cls: 'd', text: `${d} DAYS` }
  return { cls: 'v', text: 'ACTIVE' }
}

function ts(s: string): string {
  return s ? new Date(s).toLocaleString() : '—'
}

// Ported from internal/web/templates/api_keys.html against GET/POST
// /api/settings/api-keys and .../{id}/revoke (internal/api/settingsservice.go),
// laid out as design/'s screen 8g: the state-filtered key table with selection
// driving a Revoke footer, and the selected key's detail expanded under its own
// row. The raw
// token comes back once, in the create response, and is never fetched again.
// Omitted from that screen: the "Scope" column and the detail's scope/clusters
// rows as design/ draws them (a Bearer token authenticates as its owning user —
// internal/api/middleware.go's AuthenticateAPIToken — so there is no per-key
// route allow-list to show; the detail says what the scope actually is), the
// "Requests 24h" column and the "Recent calls" panel (nothing counts calls per
// token; last_used_at is the only usage the store keeps, shown as its own
// column), "rate limit" and "source IPs" (neither is enforced), and "Set expiry"
// (expiry is fixed at creation — there is no update endpoint, only revoke).
export function ApiKeysPage() {
  const { data: session } = useSession()
  const isAdmin = session?.user?.role === 'admin'
  const [stateFilter, setStateFilter] = useState('')
  const [filter, setFilter] = useState('')
  const [picked, setPicked] = useState<bigint | null>(null)
  const [adding, setAdding] = useState(false)
  const [form, setForm] = useState({ name: '', expiresDays: 365 })
  const [createdToken, setCreatedToken] = useState('')
  const { data, isPending, isError, error } = useAPIKeys(stateFilter)
  const create = useCreateAPIKey()
  const revoke = useRevokeAPIKey()

  const all = data?.keys ?? []
  const needle = filter.trim().toLowerCase()
  const rows = needle
    ? all.filter((k) => k.name.toLowerCase().includes(needle) || k.username.toLowerCase().includes(needle))
    : all
  const sel = rows.find((k) => k.id === picked)

  const columns: Column<ApiKeyItem>[] = [
    {
      name: 'Name', width: 190,
      render: (k) => <span className="m" style={k.id === picked ? { fontWeight: 500 } : undefined}><Disclosure open={k.id === picked} />{k.name}</span>,
    },
    { name: 'Prefix', width: 130, render: (k) => <span className="m mu">{k.prefix ? `${k.prefix}…` : '—'}</span> },
    { name: 'Owner', width: 150, render: (k) => <span className="m mu">{k.username}</span> },
    { name: 'Created', render: (k) => <span className="m mu">{ts(k.createdAt)}</span> },
    { name: 'Last used', width: 150, render: (k) => <span className={k.lastUsedAt ? 'm mu' : 'm mus'}>{k.lastUsedAt ? ts(k.lastUsedAt) : 'never'}</span> },
    { name: 'Expires', width: 110, render: (k) => <span className={k.expiresAt ? 'm mu' : 'm mus'}>{k.expiresAt ? new Date(k.expiresAt).toLocaleDateString() : 'no expiry'}</span> },
    {
      name: 'State',
      width: 96,
      render: (k) => {
        const s = state(k)
        return <Badge cls={s.cls}>{s.text}</Badge>
      },
    },
  ]

  return (
    <SettingsLayout
      title="API keys"
      actions={
        <>
          <span className="m mus">a key carries its owner's role — nothing more</span>
          {isAdmin && (
            <Button small primary onClick={() => setAdding((v) => !v)}>
              {adding ? 'Cancel' : 'Create key'}
            </Button>
          )}
        </>
      }
    >
      <div className="col">
        {createdToken && (
          <Panel style={{ borderColor: '#f2cd8e', background: '#fdf6ec' }}>
            <div className="lbl" style={{ marginBottom: 5 }}>Copy this token now — it is never shown again</div>
            <div className="m" style={{ wordBreak: 'break-all' }}>{createdToken}</div>
            <div className="m mus" style={{ marginTop: 5 }}>
              Only the prefix is stored, so the key cannot be recovered from the index — a lost token means a new key.
            </div>
          </Panel>
        )}

        {isPending && <Loading label="Loading API keys…" />}
        {isError && <div className="m" style={{ color: '#a1231c' }}>{error.message}</div>}

        {data && (
          <Panel z>
            <PanelHeader
              title="Keys"
              meta="expand a key to see who owns it, when it was last used and what it can reach"
              actions={
                <>
                  <span className="fld" style={{ flex: '0 0 150px' }}>
                    <span className="m mus">filter</span>
                    <input value={filter} onChange={(e) => setFilter(e.target.value)} />
                  </span>
                  <Select
                    options={[
                      { value: '', text: 'State: any' },
                      { value: 'active', text: 'State: active' },
                      { value: 'expired', text: 'State: expired' },
                      { value: 'revoked', text: 'State: revoked' },
                    ]}
                    value={stateFilter}
                    onChange={(v) => { setStateFilter(v); setPicked(null) }}
                  />
                </>
              }
            />
            <Table
              items={rows}
              columns={columns}
              rowKey={(k) => k.id.toString()}
              rowClassName={(k) => (k.id === picked ? 'hl' : '')}
              onRowClick={(k) => setPicked(k.id === picked ? null : k.id)}
              emptyMessage="No API keys match this filter."
              renderExpanded={(k) => k.id === picked && (
                <div className="kv m" style={{ display: 'grid', gridTemplateColumns: '104px 1fr', gap: '5px 8px' }}>
                  <span className="mus">created</span><span>{ts(k.createdAt)} by {k.username}</span>
                  <span className="mus">prefix</span><span>{k.prefix ? `${k.prefix}…` : '—'}</span>
                  <span className="mus">secret</span><span className="mus">shown once at creation · only the prefix is stored</span>
                  <span className="mus">scope</span><span>every endpoint {k.username} can reach — a key is that user</span>
                  <span className="mus">last used</span><span className={k.lastUsedAt ? undefined : 'mus'}>{k.lastUsedAt ? ts(k.lastUsedAt) : 'never'}</span>
                  <span className="mus">expires</span><span>{k.expiresAt ? ts(k.expiresAt) : 'never'}</span>
                  {k.revokedAt && <><span className="mus">revoked</span><span>{ts(k.revokedAt)}</span></>}
                </div>
              )}
            />
            <PanelFooter>
              <span className="m mus">{sel ? '1 selected' : 'pick a key to see its detail'}</span>
              {sel && isAdmin && !sel.revokedAt && (
                <Button small danger loading={revoke.isPending} onClick={() => revoke.mutate(sel.id, { onSuccess: () => setPicked(null) })}>
                  Revoke
                </Button>
              )}
              {revoke.isError && <span className="m" style={{ color: '#a1231c' }}>{revoke.error.message}</span>}
              <div style={{ flex: 1 }} />
              <span className="m mus">{rows.length} of {all.length}</span>
            </PanelFooter>
          </Panel>
        )}

        {adding && isAdmin && (
          <Panel style={{ maxWidth: 360 }}>
            <div className="lbl" style={{ marginBottom: 7 }}>New key</div>
            <div className="col" style={{ gap: 7 }}>
              <div className="col" style={{ gap: 3 }}>
                <span className="m mus">name</span>
                <Field value={form.name} onChange={(v) => setForm({ ...form, name: v })} grow />
              </div>
              <div className="col" style={{ gap: 3 }}>
                <span className="m mus">expires in days · 1–3,650</span>
                <Field type="number" value={String(form.expiresDays)} onChange={(v) => setForm({ ...form, expiresDays: Number(v) })} />
              </div>
              {create.isError && <div className="m" style={{ color: '#a1231c' }}>{create.error.message}</div>}
              <div className="row">
                <Button small primary loading={create.isPending} onClick={() => create.mutate(form, {
                  onSuccess: (res) => {
                    setCreatedToken(typeof res.token === 'string' ? res.token : '')
                    setForm({ name: '', expiresDays: 365 })
                    setAdding(false)
                  },
                })}>
                  Create
                </Button>
                <Button small onClick={() => setAdding(false)}>Cancel</Button>
              </div>
            </div>
          </Panel>
        )}
      </div>
    </SettingsLayout>
  )
}
