import type { ReactNode } from 'react'
import './QueryBar.css'

// The filter strip design/ puts between .ptitle and .bd on every list screen
// (3a, 2c, 2f, 2l, 6b, 2b, 2g…): full-bleed white, its own bottom border.
export function QueryBar({ children }: { children: ReactNode }) {
  return <div className="qbar">{children}</div>
}
