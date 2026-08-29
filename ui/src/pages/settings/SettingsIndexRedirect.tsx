import { Navigate } from 'react-router-dom'
import { useSession } from '../../api/queries/session'

// Ports internal/web/settings.go's settingsIndex: /settings has no content
// of its own, so it sends the viewer to the first section they can read —
// Credentials for an admin, Audit log (the one viewer-readable section) for
// everyone else.
export function SettingsIndexRedirect() {
  const { data: session, isPending } = useSession()
  if (isPending) return null
  return <Navigate to={session?.user?.role === 'admin' ? '/settings/credentials' : '/settings/audit'} replace />
}
