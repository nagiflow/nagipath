import type { ReactNode } from 'react'
import { Panel } from './Panel'

// Composes Panel for its .pnl chrome rather than hardcoding the class —
// no CSS of its own.
export function EmptyPrompt({ title, body, danger }: { title: ReactNode; body?: ReactNode; danger?: boolean }) {
  return (
    <Panel style={{ textAlign: 'center', padding: 32, borderColor: danger ? '#f0b3ad' : undefined }}>
      <div style={{ font: '700 16px Inter', color: danger ? '#a1231c' : '#1d1e24', marginBottom: 6 }}>{title}</div>
      {body && <div className="m mu">{body}</div>}
    </Panel>
  )
}
