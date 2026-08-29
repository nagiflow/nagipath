// Mirrors internal/web/nav.go's one nav table — group, item, href, and which
// count from SessionResponse.nav_counts each item shows. Kept as one file for
// the same reason nav.go is one function: the sidebar and the breadcrumb must
// never be able to disagree about what page you're on.
// The real data fields of NavCounts, not protobuf-ES's bookkeeping ones
// ($typeName, $unknown) that a bare `keyof NavCounts` would also pick up.
type NavCountKey = 'nodes' | 'sites' | 'clusters' | 'drift' | 'certificates'

export interface NavItem {
  label: string
  href: string
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
      { label: 'Dashboard', href: '/' },
      { label: 'Trace', href: '/trace' },
      { label: 'Rule lookup', href: '/rules' },
      { label: 'Config search', href: '/search' },
    ],
  },
  {
    section: 'Inventory',
    items: [
      { label: 'Sites', href: '/sites', countKey: 'sites' },
      { label: 'Nodes', href: '/nodes', countKey: 'nodes' },
      { label: 'Clusters', href: '/clusters', countKey: 'clusters' },
    ],
  },
  {
    section: 'Analysis',
    items: [
      { label: 'Drift', href: '/drift', countKey: 'drift', warnOnCount: true },
      { label: 'Certificates', href: '/certificates', countKey: 'certificates', warnOnCount: true },
    ],
  },
]

export const settingsHref = '/settings'
