import type { ReactNode } from 'react'

// The strip design/ pins to the bottom of a .pnl.z (row counts, bulk actions,
// pagination) — margin-top:auto so it sticks to the panel floor. No CSS class
// of its own: it's a layout rule on plain inline style, not a visual pattern
// like the rest of this design system's classes.
export function PanelFooter({ children }: { children: ReactNode }) {
  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 10, padding: '7px 12px', borderTop: '1px solid #d3dae6', marginTop: 'auto' }}>
      {children}
    </div>
  )
}
