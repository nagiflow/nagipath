// Mirrors internal/web/nav.go's one nav table — group, item, href, and which
// count from SessionResponse.nav_counts each item shows. Kept as one file for
// the same reason nav.go is one function: the sidebar and the breadcrumb must
// never be able to disagree about what page you're on.
// The real data fields of NavCounts, not protobuf-ES's bookkeeping ones
// ($typeName, $unknown) that a bare `keyof NavCounts` would also pick up.
import type { NavIconName } from '../ui/icons'

type NavCountKey = 'nodes' | 'sites' | 'clusters' | 'drift' | 'certificates'


export interface NavItem {
  label: string
  href: string
  icon: NavIconName
  countKey?: NavCountKey
  warnOnCount?: boolean
}

export interface NavGroup {
  section: string
  items: NavItem[]
}

export const navGroups: NavGroup[] = [
  {
    section: 'Explore',
    items: [
      { label: 'Dashboard', href: '/', icon: 'dashboard' },
      { label: 'Trace', href: '/trace', icon: 'trace' },
      { label: 'Rule lookup', href: '/rules', icon: 'ruleLookup' },
      { label: 'Config search', href: '/search', icon: 'search' },
    ],
  },
  {
    section: 'Inventory',
    items: [
      { label: 'Sites', href: '/sites', icon: 'globe', countKey: 'sites' },
      { label: 'Nodes', href: '/nodes', icon: 'server', countKey: 'nodes' },
    ],
  },
  {
    section: 'Analysis',
    items: [
      { label: 'Drift', href: '/drift', icon: 'drift', countKey: 'drift', warnOnCount: true },
      { label: 'Certificates', href: '/certificates', icon: 'shield', countKey: 'certificates', warnOnCount: true },
    ],
  },
]

export const settingsHref = '/settings'

// Ports internal/web/nav.go's settingsNav: the second-level list on Settings,
// sections a viewer may not open dropped rather than disabled. Lives here, not
// in SettingsLayout, so the breadcrumb names the sub-page ("Settings /
// Collection jobs", design/'s 8a-8h) from the same table that renders the list
// — including the two sub-pages that sit outside /settings/.
export const settingsItems: { label: string; href: string; icon: NavIconName; admin?: boolean }[] = [
  { label: 'Credentials', href: '/settings/credentials', icon: 'key', admin: true },
  { label: 'Host keys', href: '/settings/hostkeys', icon: 'lock', admin: true },
  { label: 'Master key', href: '/settings/masterkey', icon: 'vault', admin: true },
  { label: 'Collection defaults', href: '/settings/collection-defaults', icon: 'sliders', admin: true },
  { label: 'Collection jobs', href: '/collections', icon: 'refresh', admin: true },
  { label: 'Retention', href: '/settings/retention', icon: 'archive', admin: true },
  { label: 'Users & roles', href: '/settings/users', icon: 'users', admin: true },
  { label: 'API keys', href: '/settings/api-keys', icon: 'apiKeys', admin: true },
  { label: 'Audit log', href: '/settings/audit', icon: 'auditLog' },
  { label: 'License', href: '/settings/license', icon: 'shield' },
  { label: 'System', href: '/settings/system', icon: 'settings', admin: true },
]

// Header breadcrumb (design/'s "Explore / Trace" pattern): longest-href-prefix
// match against the same tables the sidebar and the Settings sub-nav render
// from, so they can never disagree about what page you're on. Detail/sub-routes
// (e.g. /nodes/42) fall back to their list item; anything else (login,
// password, /nope) returns null and the header just omits the crumb.
type Crumb = { section?: string; label: string; href: string }

export function breadcrumbFor(pathname: string): { section?: string; label: string } | null {
  const found: Crumb[] = []
  const consider = (c: Crumb) => {
    const matches = c.href === '/' ? pathname === '/' : pathname === c.href || pathname.startsWith(c.href + '/')
    if (matches) found.push(c)
  }
  for (const group of navGroups) {
    for (const item of group.items) consider({ section: group.section, label: item.label, href: item.href })
  }
  consider({ label: 'Settings', href: settingsHref })
  for (const item of settingsItems) consider({ section: 'Settings', label: item.label, href: item.href })
  // Snapshots is reached from a node, not from the sidebar, so it has no nav row
  // to borrow a crumb from — but a page with no crumb at all reads as broken.
  consider({ section: 'Inventory', label: 'Snapshots', href: '/snapshots' })
  const best = found.sort((a, b) => b.href.length - a.href.length)[0]
  return best ? { section: best.section, label: best.label } : null
}
