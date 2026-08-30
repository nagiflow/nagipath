import type { ReactNode } from 'react'
import { Panel } from './Panel'

// Composes Panel for its .pnl chrome rather than hardcoding the class —
// no CSS of its own beyond the fixed backdrop, which is layout, not a
// reusable visual pattern.
export function Modal({ title, onClose, children, footer }: { title: ReactNode; onClose: () => void; children: ReactNode; footer?: ReactNode }) {
  return (
    <div
      style={{ position: 'fixed', inset: 0, background: 'rgba(29,30,36,.4)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 1000 }}
      onClick={onClose}
    >
      <Panel style={{ width: 420, background: '#fff' }} onClick={(e) => e.stopPropagation()}>
        <div className="ph2" style={{ marginBottom: 10 }}>{title}</div>
        {children}
        {footer && <div className="row" style={{ marginTop: 14, justifyContent: 'flex-end' }}>{footer}</div>}
      </Panel>
    </div>
  )
}
