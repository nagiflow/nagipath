// design/'s "Since" column: "21m", "3h 12m" while it is recent, then a plain
// date ("Aug 19") once it stops being a stopwatch reading.
export function since(iso: string): string {
  if (!iso) return '—'
  const then = new Date(iso)
  const mins = Math.max(0, Math.round((Date.now() - then.getTime()) / 60000))
  if (mins < 60) return `${mins}m`
  if (mins < 1440) {
    const h = Math.floor(mins / 60)
    const m = mins % 60
    return m ? `${h}h ${m}m` : `${h}h`
  }
  return then.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
}

// Whole days until an expiry, negative once it has passed.
export function daysUntil(iso: string): number {
  return Math.floor((new Date(iso).getTime() - Date.now()) / 86400e3)
}
