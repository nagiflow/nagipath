import type { ReactNode } from 'react'
import './Chip.css'

export function Chip({ children }: { children: ReactNode }) {
  return <span className="chip">{children}</span>
}
