import { useState } from 'react'
import { Navigate } from 'react-router-dom'
import { useSession, useSetup } from '../../api/queries/session'
import { Button, Field, Loading } from '../../components/ui'
import { AuthCard } from './AuthCard'

// Ported from internal/web/templates/setup.html (docs/adr/0017, Phase 8).
// Reachable only while the database has no users: once one exists,
// GET /api/session's setup_required flips to false and this redirects to
// /login — POST /api/setup itself also refuses a second call regardless.
export function SetupPage() {
  const { data: session, isPending } = useSession()
  const setup = useSetup()
  const [form, setForm] = useState({ username: '', password: '', confirm: '' })

  if (isPending) return <Loading label="Loading…" />
  if (session?.authenticated) return <Navigate to="/" replace />
  if (session && !session.setupRequired) return <Navigate to="/login" replace />

  return (
    <AuthCard title="Set up nagipath">
      <form
        className="col"
        onSubmit={(e) => {
          e.preventDefault()
          setup.mutate(form)
        }}
      >
        <div className="col" style={{ gap: 4 }}>
          <span className="lbl">Username</span>
          <Field value={form.username} onChange={(v) => setForm({ ...form, username: v })} grow />
        </div>
        <div className="col" style={{ gap: 4 }}>
          <span className="lbl">Password</span>
          <Field type="password" value={form.password} onChange={(v) => setForm({ ...form, password: v })} grow />
          <span className="m mus">At least 12 characters — there is no second chance if it's lost.</span>
        </div>
        <div className="col" style={{ gap: 4 }}>
          <span className="lbl">Confirm password</span>
          <Field type="password" value={form.confirm} onChange={(v) => setForm({ ...form, confirm: v })} grow />
        </div>
        {setup.isError && <div className="m" style={{ color: '#a1231c' }}>{setup.error.message}</div>}
        <Button primary type="submit" loading={setup.isPending}>Create the first administrator</Button>
      </form>
    </AuthCard>
  )
}
