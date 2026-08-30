import { Fragment, type ReactNode } from 'react'
import './Steps.css'

// The numbered progress bar a multi-step page (Import inventory, screen 2q)
// puts between .ptitle and .bd — done steps get a filled dot and a check,
// the current one an outlined dot, later ones stay flat.
export function Steps({ steps, current }: { steps: ReactNode[]; current: number }) {
  return (
    <div className="stp">
      {steps.map((label, i) => (
        <Fragment key={i}>
          {i > 0 && <div className="stp-sep" />}
          <div className={`stp-i${i < current ? ' done' : i === current ? ' on' : ''}`}>
            <span className="stp-n">{i < current ? '✓' : i + 1}</span>
            {label}
          </div>
        </Fragment>
      ))}
    </div>
  )
}
