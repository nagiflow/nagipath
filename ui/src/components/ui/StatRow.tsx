import type { ReactNode } from 'react'
import './StatRow.css'

export interface StatItem { label: string; value: ReactNode; sub?: ReactNode; tone?: 'warning' | 'danger' }

export function StatRow({ stats }: { stats: StatItem[] }) {
  return (
    <div className="row" style={{ flex: 'none' }}>
      {stats.map((s, i) => (
        <div className="stat" key={i} style={s.tone === 'warning' ? { borderColor: '#f2cd8e' } : s.tone === 'danger' ? { borderColor: '#f0b3ad' } : undefined}>
          <span className="lbl" style={s.tone === 'warning' ? { color: '#8a5300' } : s.tone === 'danger' ? { color: '#a1231c' } : undefined}>{s.label}</span>
          <span className="num" style={s.tone === 'warning' ? { color: '#8a5300' } : s.tone === 'danger' ? { color: '#a1231c' } : undefined}>{s.value}</span>
          {s.sub && <span className="m mus">{s.sub}</span>}
        </div>
      ))}
    </div>
  )
}
