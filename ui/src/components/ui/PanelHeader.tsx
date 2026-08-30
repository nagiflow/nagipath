import type { ReactNode } from 'react'
import './PanelHeader.css'

export function PanelHeader({ title, meta, actions }: { title: ReactNode; meta?: ReactNode; actions?: ReactNode }) {
  return (
    <div className="phd">
      <h2 className="ph2">{title}</h2>
      {meta && <span className="m mus">{meta}</span>}
      <div style={{ flex: 1 }} />
      {actions}
    </div>
  )
}
