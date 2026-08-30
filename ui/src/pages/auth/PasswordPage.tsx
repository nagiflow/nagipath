import { useState } from 'react'
import { Navigate } from 'react-router-dom'
import { useChangePassword, useSession } from '../../api/queries/session'
import { PageHeader } from '../../components/shared/PageHeader'
import { Button, CallOut, Field, Panel } from '../../components/ui'

// Ported from internal/web/templates/password.html (docs/adr/0017, Phase 8),
// laid out as design/'s screen 8k.
// Rendered inside AppShell like any other authenticated page — internal/web's
// auth() middleware still forces every OTHER page to redirect here while
// must_change_password is set (web.go, unchanged by this port), so this page
// itself needs no route guard for that case.
export function PasswordPage() {
  const { data: session } = useSession()
  const changePassword = useChangePassword()
  const [form, setForm] = useState({ current: '', new: '', confirm: '' })

  if (!session) return null
  if (changePassword.isSuccess) return <Navigate to="/" replace />

  return (
    <>
      <PageHeader title="Change password" meta={`${session.user?.username} · ${session.user?.role}`} />
      <div className="bd">
        <Panel style={{ maxWidth: 420 }}>
          <form
            className="col"
            onSubmit={(e) => {
              e.preventDefault()
              changePassword.mutate(form)
            }}
          >
            {session.mustChangePassword && (
              <CallOut color="warning" title="Temporary password">
                An administrator set a temporary password for this account. Choose a new one to continue.
              </CallOut>
            )}
            {!session.mustChangePassword && (
              <div className="col" style={{ gap: 4 }}>
                <span className="lbl">Current password</span>
                <Field type="password" value={form.current} onChange={(v) => setForm({ ...form, current: v })} grow />
              </div>
            )}
            <div className="col" style={{ gap: 4 }}>
              <span className="lbl">New password</span>
              <Field type="password" value={form.new} onChange={(v) => setForm({ ...form, new: v })} grow />
              <span className="m mus">At least 12 characters.</span>
            </div>
            <div className="col" style={{ gap: 4 }}>
              <span className="lbl">Confirm new password</span>
              <Field type="password" value={form.confirm} onChange={(v) => setForm({ ...form, confirm: v })} grow />
            </div>
            {changePassword.isError && <div className="m" style={{ color: '#a1231c' }}>{changePassword.error.message}</div>}
            {/* design/'s 8k puts the action at its natural width, unlike the
                signed-out card (9a/9b) where it fills the card. */}
            <div><Button primary type="submit" loading={changePassword.isPending}>Change password</Button></div>
          </form>
        </Panel>
      </div>
    </>
  )
}
