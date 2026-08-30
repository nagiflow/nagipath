import type { ReactNode } from 'react'
import { Panel } from './Panel'

// Composes Panel for its .pnl chrome rather than hardcoding the class —
// no CSS of its own.
export function CallOut({ title, color, children }: { title: ReactNode; color?: 'warning' | 'danger' | 'success'; children?: ReactNode }) {
  const border = color === 'danger' ? '#f0b3ad' : color === 'success' ? '#a3d4cf' : '#f2cd8e'
  const bg = color === 'danger' ? '#fceeed' : color === 'success' ? '#e6f2f1' : '#fdf3e2'
  return (
    <Panel style={{ background: bg, borderColor: border, padding: '9px 12px' }}>
      <div style={{ font: '600 12.5px Inter', color: '#1d1e24' }}>{title}</div>
      {children && <div className="m mu" style={{ marginTop: 4 }}>{children}</div>}
    </Panel>
  )
}
