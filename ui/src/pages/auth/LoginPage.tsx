import { useState } from 'react'
import { Navigate, useSearchParams } from 'react-router-dom'
import { useLogin, useSession } from '../../api/queries/session'
import { Button, Field, Loading } from '../../components/ui'
import { AuthCard } from './AuthCard'

function safeNext(next: string | null): string {
  return next && next.startsWith('/') ? next : '/'
}

// Ported from internal/web/templates/login.html (docs/adr/0017, Phase 8).
// Bare, outside AppShell: there is no session to draw its chrome from yet.
export function LoginPage() {
  const { data: session, isPending } = useSession()
  const [searchParams] = useSearchParams()
  const login = useLogin()
  const [form, setForm] = useState({ username: '', password: '' })

  if (isPending) return <Loading label="Loading…" />
  if (session?.authenticated) return <Navigate to={safeNext(searchParams.get('next'))} replace />
  if (session?.setupRequired) return <Navigate to="/setup" replace />

  return (
    <AuthCard title="Sign in">
      <form
        className="col"
        onSubmit={(e) => {
          e.preventDefault()
          login.mutate(form)
        }}
      >
        <div className="col" style={{ gap: 4 }}>
          <span className="lbl">Username</span>
          <Field value={form.username} onChange={(v) => setForm({ ...form, username: v })} grow />
        </div>
        <div className="col" style={{ gap: 4 }}>
          <span className="lbl">Password</span>
          <Field type="password" value={form.password} onChange={(v) => setForm({ ...form, password: v })} grow />
        </div>
        {login.isError && <div className="m" style={{ color: '#a1231c' }}>{login.error.message}</div>}
        <Button primary type="submit" loading={login.isPending}>Sign in</Button>
      </form>
    </AuthCard>
  )
}
