import type { ReactNode } from 'react'
import './Badge.css'

const bucketClass: Record<string, string> = {
  verified: 'v', conforming: 'v', succeeded: 'v', approved: 'v', valid: 'v', ok: 'v', parsed: 'v', completed: 'v',
  inferred: 'i', candidate: 'i', missing: 'i',
  observed_effect: 'd', observed: 'd', partial: 'd', degraded: 'd', expiring: 'd', stale: 'd', pending: 'd',
  drift: 'd', over_ceiling: 'd', running: 'd',
  external_hop: 'e',
  disproved: 'r', failed: 'r', expired: 'r', quarantined: 'r', denied: 'r', rejected: 'r', blocked: 'r',
}

export function badgeClass(state: string): string {
  return bucketClass[state] ?? 'n'
}

// `cls` overrides the state->bucket mapping with an explicit .v/.i/.d/.e/.r/.n
// letter, for callers that already know the right bucket (dashboard tone
// codes, table state columns) rather than a StateBadge-style status word.
export function Badge({ state, cls, className, title, children }: { state?: string; cls?: string; className?: string; title?: string; children?: ReactNode }) {
  const bucket = cls ?? (state ? badgeClass(state) : 'n')
  return <span className={`bg ${bucket}${className ? ` ${className}` : ''}`} title={title}>{children ?? state?.toUpperCase().replace(/_/g, ' ')}</span>
}
