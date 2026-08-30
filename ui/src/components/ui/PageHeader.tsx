import type { ReactNode } from 'react'
import './PageHeader.css'

export function PageHeader({ title, badge, meta, actions }: { title: ReactNode; badge?: ReactNode; meta?: ReactNode; actions?: ReactNode }) {
  return (
    <div className="ptitle">
      <span className="h1">{title}</span>
      {badge}
      {meta && <span className="m mu">{meta}</span>}
      <div style={{ flex: 1 }} />
      {actions}
    </div>
  )
}
