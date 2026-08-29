import { EuiFlexGroup, EuiFlexItem, EuiSpacer, EuiText } from '@elastic/eui'
import { Link, useLocation } from 'react-router-dom'
import { useSession } from '../../api/queries/session'

// Ports internal/web/nav.go's settingsNav: the second-level list on
// Settings, sections a viewer may not open dropped rather than disabled.
const items: { label: string; href: string; admin?: boolean }[] = [
  { label: 'Credentials', href: '/settings/credentials', admin: true },
  { label: 'Host keys', href: '/settings/hostkeys', admin: true },
  { label: 'Master key', href: '/settings/masterkey', admin: true },
  { label: 'Collection defaults', href: '/settings/collection-defaults', admin: true },
  { label: 'Collection jobs', href: '/collections', admin: true },
  { label: 'Retention', href: '/settings/retention', admin: true },
  { label: 'Users & roles', href: '/settings/users', admin: true },
  { label: 'API keys', href: '/settings/api-keys', admin: true },
  { label: 'Audit log', href: '/settings/audit' },
  { label: 'License', href: '/settings/license' },
  { label: 'System', href: '/settings/system', admin: true },
]

export function SettingsSubNav() {
  const { data: session } = useSession()
  const { pathname } = useLocation()
  const isAdmin = session?.user?.role === 'admin'

  return (
    <>
      <EuiFlexGroup gutterSize="m" wrap responsive={false}>
        {items
          .filter((it) => !it.admin || isAdmin)
          .map((it) => (
            <EuiFlexItem grow={false} key={it.href}>
              <Link to={it.href}>
                <EuiText size="s" color={pathname === it.href ? 'default' : 'subdued'}>
                  {pathname === it.href ? <strong>{it.label}</strong> : it.label}
                </EuiText>
              </Link>
            </EuiFlexItem>
          ))}
      </EuiFlexGroup>
      <EuiSpacer />
    </>
  )
}
