import { useState, type ReactNode } from 'react'
import { Button } from './Button'

// Composes Button for its toggle rather than hardcoding `.btn s sm` — no
// CSS of its own.
export function Accordion({ title, initialOpen, children }: { title: ReactNode; initialOpen?: boolean; children: ReactNode }) {
  const [open, setOpen] = useState(!!initialOpen)
  return (
    <div>
      <div style={{ marginBottom: open ? 6 : 0 }}>
        <Button small subtle onClick={() => setOpen((o) => !o)}>{open ? '▾' : '▸'} {title}</Button>
      </div>
      {open && children}
    </div>
  )
}
