import { useRef } from 'react'
import './Resizer.css'

// The draggable divider between two panes of a Finder-style cascade. It replaces
// the 1px border it sits in place of, so the column keeps the same seam it had.
//
// The drag uses pointer capture rather than document-level listeners: the
// pointer keeps reporting to this element once it leaves it, which is what makes
// a fast drag across the next pane keep resizing instead of stopping dead.
export function Resizer({ width, onChange, min = 120, max = 640, step = 16 }: {
  width: number
  onChange: (w: number) => void
  min?: number
  max?: number
  step?: number
}) {
  const start = useRef<{ x: number; w: number } | null>(null)
  const clamp = (w: number) => Math.min(max, Math.max(min, w))

  return (
    <div
      className="rsz"
      role="separator"
      aria-orientation="vertical"
      aria-valuenow={width}
      aria-valuemin={min}
      aria-valuemax={max}
      tabIndex={0}
      onPointerDown={(e) => {
        start.current = { x: e.clientX, w: width }
        e.currentTarget.setPointerCapture(e.pointerId)
      }}
      onPointerMove={(e) => {
        if (start.current) onChange(clamp(start.current.w + e.clientX - start.current.x))
      }}
      onPointerUp={(e) => {
        start.current = null
        e.currentTarget.releasePointerCapture(e.pointerId)
      }}
      onKeyDown={(e) => {
        if (e.key === 'ArrowLeft') onChange(clamp(width - step))
        else if (e.key === 'ArrowRight') onChange(clamp(width + step))
        else return
        e.preventDefault()
      }}
    />
  )
}
