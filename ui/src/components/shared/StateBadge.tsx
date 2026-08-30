import { Badge } from '../ui'

// Thin wrapper — see Badge (components/ui/Badge.tsx) for the actual .bg
// pill rendering and the state->color-bucket mapping.
export function StateBadge({ state, className, title }: { state: string; className?: string; title?: string }) {
  return <Badge state={state} className={className} title={title} />
}
