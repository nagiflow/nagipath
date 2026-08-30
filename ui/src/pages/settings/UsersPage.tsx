import { useState } from 'react'
import { useSession } from '../../api/queries/session'
import { useAddUser, useSetUserDisabled, useUsers } from '../../api/queries/settings'
import type { UserItem } from '../../api/pb/nagipath/api/v1/settings_pb'
import { PanelHeader } from '../../components/shared/PanelHeader'
import {
  Badge, Button, Checkbox, Field, Loading, Panel, PanelFooter, Select, Table,
  type Column,
} from '../../components/ui'
import { SettingsLayout } from './SettingsLayout'

function ts(s: string): string {
  return s ? new Date(s).toLocaleString() : '—'
}

// Ported from internal/web/templates/users.html against GET/POST
// /api/settings/users and .../{id}/disable|enable (internal/api/settingsservice.go),
// laid out as design/'s screen 2v: the Users & roles table with the role
// capabilities strip under it, and the create form beside its own title-bar
// button.
// Omitted from that screen: the "Source: SAML · corp-okta" select (authentication
// is a local password against app_user — there is no identity provider to name),
// the "Scope" column and "app-*, edge-*" globs (a role is global; nothing checks
// a per-user cluster scope server-side), "Invite" as an invite flow (an admin
// creates the account with a password and can require a change at first login —
// no mail is sent), the per-row "Edit" (there is no role-change endpoint; a role
// is set at creation, and disable is the reversible action that exists), and the
// Operator/Service roles (the model has two: viewer reads, admin mutates).
export function UsersPage() {
  const { data: session } = useSession()
  const { data, isPending, isError, error } = useUsers()
  const add = useAddUser()
  const setDisabled = useSetUserDisabled()
  const [adding, setAdding] = useState(false)
  const [form, setForm] = useState({ username: '', password: '', confirm: '', role: 'viewer', mustChange: false })

  const isAdmin = session?.user?.role === 'admin'
  const users = data?.users ?? []

  const columns: Column<UserItem>[] = [
    {
      name: 'User',
      width: 200,
      render: (u) => (
        <span className="m">
          {u.username}
          {u.username === session?.user?.username && <span className="mus"> · you</span>}
        </span>
      ),
    },
    { name: 'Role', width: 110, render: (u) => <span className="m mu">{u.role}</span> },
    { name: 'Added', width: 150, render: (u) => <span className="m mu">{ts(u.createdAt)}</span> },
    {
      name: 'Last active',
      width: 150,
      render: (u) => <span className={u.lastLoginAt ? 'm mu' : 'm mus'}>{u.lastLoginAt ? ts(u.lastLoginAt) : 'never signed in'}</span>,
    },
    {
      name: 'State',
      width: 110,
      render: (u) => (u.disabled
        ? <Badge cls="r">DISABLED</Badge>
        : u.mustChangePassword
          ? <Badge cls="d">MUST RESET</Badge>
          : <Badge cls="v">ACTIVE</Badge>),
    },
    {
      name: '',
      width: 80,
      render: (u) => (isAdmin ? (
        <Button small danger={!u.disabled} loading={setDisabled.isPending}
          onClick={() => setDisabled.mutate({ id: u.id, disabled: !u.disabled })}>
          {u.disabled ? 'Enable' : 'Disable'}
        </Button>
      ) : null),
    },
  ]

  return (
    <SettingsLayout
      title="Settings"
      actions={
        <>
          <span className="m mus">local accounts · changes are audited</span>
          {isAdmin && (
            <Button small primary onClick={() => setAdding((v) => !v)}>{adding ? 'Cancel' : 'Add user'}</Button>
          )}
        </>
      }
    >
      <div className="col">
        {isPending && <Loading label="Loading users…" />}
        {isError && <div className="m" style={{ color: '#a1231c' }}>{error.message}</div>}
        {setDisabled.isError && <div className="m" style={{ color: '#a1231c' }}>{setDisabled.error.message}</div>}

        {data && (
          <Panel z>
            <PanelHeader title="Users & roles" meta={`${users.length} account${users.length === 1 ? '' : 's'}`} />
            <Table items={users} columns={columns} rowKey={(u) => u.id.toString()} emptyMessage="No users yet." />
            <PanelFooter>
              <div className="col" style={{ gap: 3, flex: 1 }}>
                <div className="lbl">Role capabilities</div>
                <div className="fct"><span className="m mu" style={{ width: 74 }}>viewer</span><span className="m">read inventory, traces, rules, drift</span></div>
                <div className="fct"><span className="m mu" style={{ width: 74 }}>admin</span><span className="m">+ nodes, collections, probes, drift decisions, credentials, users, retention</span></div>
                <span className="m mus">
                  The last enabled admin cannot be disabled — the install would have no one able to change it back.
                </span>
              </div>
            </PanelFooter>
          </Panel>
        )}

        {adding && isAdmin && (
          <Panel>
            <div className="lbl" style={{ marginBottom: 7 }}>New user</div>
            <div className="row" style={{ alignItems: 'flex-start' }}>
              <div className="col" style={{ gap: 7, flex: 1, minWidth: 0 }}>
                <div className="col" style={{ gap: 3 }}>
                  <span className="m mus">username</span>
                  <Field value={form.username} onChange={(v) => setForm({ ...form, username: v })} grow />
                </div>
                <div className="col" style={{ gap: 3 }}>
                  <span className="m mus">role</span>
                  <Select options={[{ value: 'viewer', text: 'viewer' }, { value: 'admin', text: 'admin' }]}
                    value={form.role} onChange={(v) => setForm({ ...form, role: v })} />
                </div>
              </div>
              <div className="col" style={{ gap: 7, flex: 1, minWidth: 0 }}>
                <div className="col" style={{ gap: 3 }}>
                  <span className="m mus">password · at least 12 characters</span>
                  <Field type="password" value={form.password} onChange={(v) => setForm({ ...form, password: v })} grow />
                </div>
                <div className="col" style={{ gap: 3 }}>
                  <span className="m mus">confirm password</span>
                  <Field type="password" value={form.confirm} onChange={(v) => setForm({ ...form, confirm: v })} grow />
                </div>
              </div>
            </div>
            <div className="col" style={{ gap: 7, marginTop: 8 }}>
              <Checkbox
                id="must-change"
                label="Require a password change at first login"
                checked={form.mustChange}
                onChange={() => setForm({ ...form, mustChange: !form.mustChange })}
              />
              {add.isError && <div className="m" style={{ color: '#a1231c' }}>{add.error.message}</div>}
              <div className="row">
                <Button small primary loading={add.isPending} onClick={() => add.mutate(form, {
                  onSuccess: () => {
                    setForm({ username: '', password: '', confirm: '', role: 'viewer', mustChange: false })
                    setAdding(false)
                  },
                })}>
                  Create user
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
