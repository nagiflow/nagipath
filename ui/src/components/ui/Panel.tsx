import type { ReactNode } from 'react'
import './Panel.css'

export function Panel({ z, children, style, className, onClick }: {
  z?: boolean
  children: ReactNode
  style?: React.CSSProperties
  className?: string
  onClick?: (e: React.MouseEvent) => void
}) {
  return <div className={`pnl${z ? ' z' : ''}${className ? ` ${className}` : ''}`} style={style} onClick={onClick}>{children}</div>
}
