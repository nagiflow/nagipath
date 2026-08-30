import { Navigate } from 'react-router-dom'
import { useSession } from '../../api/queries/session'

// /settings has no content of its own: design/'s 2v draws the Settings
// landing as the Users & roles section, so an admin lands there and a viewer
// on Audit log — the one section they can read.
export function SettingsIndexRedirect() {
  const { data: session, isPending } = useSession()
  if (isPending) return null
  return <Navigate to={session?.user?.role === 'admin' ? '/settings/users' : '/settings/audit'} replace />
}
