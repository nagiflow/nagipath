// Minimal hand-drawn monoline icon set (24x24 viewBox, stroke=currentColor) —
// self-contained, no icon-font/CDN dependency, matching this app's geometric,
// technical visual language rather than a generic icon pack. Kept to simple
// primitives (circle/rect/line/short paths).
import type { SVGProps } from 'react'

const base: SVGProps<SVGSVGElement> = {
  viewBox: '0 0 24 24',
  fill: 'none',
  stroke: 'currentColor',
  strokeWidth: 2,
  strokeLinecap: 'round',
  strokeLinejoin: 'round',
}

export function DashboardIcon() {
  return (
    <svg {...base}>
      <rect x="3" y="3" width="7" height="9" rx="1" /><rect x="14" y="3" width="7" height="5" rx="1" />
      <rect x="14" y="12" width="7" height="9" rx="1" /><rect x="3" y="16" width="7" height="5" rx="1" />
    </svg>
  )
}

export function TraceIcon() {
  return (
    <svg {...base}>
      <circle cx="5" cy="6" r="2" /><circle cx="12" cy="18" r="2" /><circle cx="19" cy="6" r="2" />
      <path d="M7 6h5M12 16v-4l5-4" />
    </svg>
  )
}

export function RuleLookupIcon() {
  return (
    <svg {...base}>
      <rect x="5" y="3" width="14" height="18" rx="1.5" /><path d="m9 12 2.5 2.5L16 10" />
    </svg>
  )
}

export function SearchIcon() {
  return (
    <svg {...base}>
      <circle cx="11" cy="11" r="7" /><path d="m21 21-4.3-4.3" />
    </svg>
  )
}

export function GlobeIcon() {
  return (
    <svg {...base}>
      <circle cx="12" cy="12" r="9" /><path d="M3 12h18" /><path d="M12 3c3 3 3 15 0 18M12 3c-3 3-3 15 0 18" />
    </svg>
  )
}

export function ServerIcon() {
  return (
    <svg {...base}>
      <rect x="3" y="4" width="18" height="7" rx="1.5" /><rect x="3" y="13" width="18" height="7" rx="1.5" />
      <circle cx="7" cy="7.5" r="0.6" fill="currentColor" /><circle cx="7" cy="16.5" r="0.6" fill="currentColor" />
    </svg>
  )
}

export function ClustersIcon() {
  return (
    <svg {...base}>
      <rect x="3" y="10" width="8" height="8" rx="1" /><rect x="9" y="5" width="8" height="8" rx="1" fill="#fff" />
      <rect x="13" y="12" width="8" height="8" rx="1" fill="#fff" />
    </svg>
  )
}

export function DriftIcon() {
  return (
    <svg {...base}>
      <path d="M4 8h11l-3-3M4 8l3 3" /><path d="M20 16H9l3 3M20 16l-3-3" />
    </svg>
  )
}

export function ShieldIcon() {
  return (
    <svg {...base}>
      <path d="M12 3 5 6v6c0 4.5 3 7.5 7 9 4-1.5 7-4.5 7-9V6l-7-3Z" /><path d="m9 12 2 2 4-4" />
    </svg>
  )
}

export function SettingsIcon() {
  return (
    <svg {...base}>
      <circle cx="12" cy="12" r="3.2" />
      <path d="M12 3v2.5M12 18.5V21M21 12h-2.5M5.5 12H3M18.4 5.6l-1.8 1.8M7.4 16.6l-1.8 1.8M18.4 18.4l-1.8-1.8M7.4 7.4 5.6 5.6" />
    </svg>
  )
}

export function KeyIcon() {
  return (
    <svg {...base}>
      <circle cx="7" cy="15" r="4" /><path d="m10 12 9-9M16 6l2.5 2.5M19 3l2 2" />
    </svg>
  )
}

export function LockIcon() {
  return (
    <svg {...base}>
      <rect x="4" y="11" width="16" height="10" rx="1.5" /><path d="M8 11V7a4 4 0 0 1 8 0v4" />
    </svg>
  )
}

export function VaultIcon() {
  return (
    <svg {...base}>
      <rect x="3" y="3" width="18" height="18" rx="1.5" /><circle cx="12" cy="12" r="3.2" /><path d="M12 8.8v.1M14.3 13.6l1.6 1" />
    </svg>
  )
}

export function SlidersIcon() {
  return (
    <svg {...base}>
      <path d="M4 6h6M14 6h6M4 12h10M18 12h2M4 18h2M10 18h10" />
      <circle cx="12" cy="6" r="2" fill="#fff" /><circle cx="16" cy="12" r="2" fill="#fff" /><circle cx="8" cy="18" r="2" fill="#fff" />
    </svg>
  )
}

export function RefreshIcon() {
  return (
    <svg {...base}>
      <path d="M4 12a8 8 0 0 1 14-5.3L21 9" /><path d="M21 4v5h-5" />
      <path d="M20 12a8 8 0 0 1-14 5.3L3 15" /><path d="M3 20v-5h5" />
    </svg>
  )
}

export function ArchiveIcon() {
  return (
    <svg {...base}>
      <rect x="3" y="4" width="18" height="4" rx="1" /><rect x="5" y="8" width="14" height="12" rx="1" /><path d="M10 12h4" />
    </svg>
  )
}

export function UsersIcon() {
  return (
    <svg {...base}>
      <circle cx="9" cy="8" r="3.2" /><path d="M3.5 19a5.6 5.6 0 0 1 11 0" />
      <circle cx="17.5" cy="9" r="2.5" /><path d="M15.5 13.2a4.6 4.6 0 0 1 5 4.3" />
    </svg>
  )
}

export function ApiKeysIcon() {
  return (
    <svg {...base}>
      <path d="m9 8-4 4 4 4M15 8l4 4-4 4" />
    </svg>
  )
}

export function AuditLogIcon() {
  return (
    <svg {...base}>
      <rect x="4" y="3" width="16" height="18" rx="1.5" /><path d="M8 8h8M8 12h8M8 16h5" />
    </svg>
  )
}

export const navIcons = {
  dashboard: DashboardIcon,
  trace: TraceIcon,
  ruleLookup: RuleLookupIcon,
  search: SearchIcon,
  globe: GlobeIcon,
  server: ServerIcon,
  clusters: ClustersIcon,
  drift: DriftIcon,
  shield: ShieldIcon,
  settings: SettingsIcon,
  key: KeyIcon,
  lock: LockIcon,
  vault: VaultIcon,
  sliders: SlidersIcon,
  refresh: RefreshIcon,
  archive: ArchiveIcon,
  users: UsersIcon,
  apiKeys: ApiKeysIcon,
  auditLog: AuditLogIcon,
} as const

export type NavIconName = keyof typeof navIcons
