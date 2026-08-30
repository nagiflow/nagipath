import { StatRow as UiStatRow, type StatItem } from '../ui'

export type Stat = StatItem

// Thin wrapper — see StatRow (components/ui/StatRow.tsx) for the actual .stat
// rendering.
export function StatRow({ stats }: { stats: Stat[] }) {
  return <UiStatRow stats={stats} />
}
