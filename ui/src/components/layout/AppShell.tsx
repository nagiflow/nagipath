import type { ReactNode } from 'react'
import {
  EuiAvatar,
  EuiBadge,
  EuiHeader,
  EuiHeaderLogo,
  EuiHeaderSectionItemButton,
  EuiIcon,
  EuiLoadingLogo,
  EuiPageTemplate,
  EuiSideNav,
  type EuiSideNavItemType,
} from '@elastic/eui'
import { useLocation } from 'react-router-dom'
import { useSession } from '../../api/queries/session'
import { logout } from '../../lib/auth'
import { navGroups, settingsHref } from './navConfig'

function useNavItems(): EuiSideNavItemType<{}>[] {
  const { data } = useSession()
  const { pathname } = useLocation()

  const groups: EuiSideNavItemType<{}>[] = navGroups.map((group) => ({
    id: group.section,
    name: group.section,
    items: group.items.map((item) => {
      const count = item.countKey && data ? data.nav_counts[item.countKey] : undefined
      return {
        id: item.href,
        name: (
          <span>
            {item.label}
            {count !== undefined && (
              <EuiBadge color={item.warnOnCount && count > 0 ? 'warning' : 'hollow'} style={{ marginLeft: 8 }}>
                {count}
              </EuiBadge>
            )}
          </span>
        ),
        href: item.href,
        isSelected: pathname === item.href,
      }
    }),
  }))

  groups.push({
    id: 'Settings',
    name: 'Settings',
    href: settingsHref,
    icon: <EuiIcon type="gear" />,
  })

  return groups
}

// AppShell is the SPA's persistent chrome (docs/adr/0017): sidebar, header,
// license banner — the same shell every ported page renders inside, driven
// entirely by GET /api/v1/session the way internal/web/nav.go's one table
// used to drive layout.html's sidebar + breadcrumb.
export function AppShell({ children }: { children: ReactNode }) {
  const { data: session, isPending, isError, error } = useSession()
  const items = useNavItems()

  if (isPending) {
    return (
      <EuiPageTemplate>
        <EuiPageTemplate.EmptyPrompt icon={<EuiLoadingLogo logo="logoElastic" size="xl" />} title={<h2>Loading…</h2>} />
      </EuiPageTemplate>
    )
  }

  if (isError) {
    return (
      <EuiPageTemplate>
        <EuiPageTemplate.EmptyPrompt iconType="alert" color="danger" title={<h2>Could not load session</h2>} body={<p>{error.message}</p>} />
      </EuiPageTemplate>
    )
  }

  return (
    <>
      <EuiHeader
        position="fixed"
        sections={[
          {
            items: [
              <EuiHeaderLogo iconType="logoElastic" href="/" key="logo">
                nagipath
              </EuiHeaderLogo>,
              ...(session.demo_mode ? [<EuiBadge color="warning" key="demo">DEMO</EuiBadge>] : []),
            ],
          },
          {
            items: [
              <EuiHeaderSectionItemButton aria-label="Sign out" onClick={() => logout(session.csrf_token)} key="avatar">
                <EuiAvatar name={session.user.username} size="s" />
              </EuiHeaderSectionItemButton>,
            ],
          },
        ]}
      />

      {session.license_notice && (
        <div style={{ background: '#fdf3e2', padding: '8px 16px', borderBottom: '1px solid #f2cd8e' }}>{session.license_notice}</div>
      )}

      <EuiPageTemplate paddingSize="none">
        <EuiPageTemplate.Sidebar sticky>
          <EuiSideNav items={items} mobileTitle="Menu" />
        </EuiPageTemplate.Sidebar>
        <EuiPageTemplate.Section>{children}</EuiPageTemplate.Section>
      </EuiPageTemplate>
    </>
  )
}
