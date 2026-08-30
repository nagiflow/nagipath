import type { ReactNode } from 'react'
import './Label.css'

export function Label({ children }: { children: ReactNode }) {
  return <span className="lbl">{children}</span>
}
