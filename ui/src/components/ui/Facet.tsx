import type { ReactNode } from 'react'
import './Facet.css'

// design/'s .fct row: a label with a right-aligned count, used for the
// upstream/facet lists in side panels.
export function Facet({ label, count }: { label: ReactNode; count?: ReactNode }) {
  return (
    <div className="fct">
      <span className="m">{label}</span>
      {count !== undefined && <span className="c">{count}</span>}
    </div>
  )
}
