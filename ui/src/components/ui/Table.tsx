import { Fragment, useLayoutEffect, useRef, useState, type ReactNode } from 'react'
import './Table.css'

export interface Column<T> { name: string; width?: number; render: (row: T) => ReactNode }

// A row's own detail, rendered as one full-width cell directly under it
// (design/'s ▾ expansion). This is the only detail affordance the app uses — a
// separate right-column or bottom preview panel puts the detail somewhere the
// eye has to travel to and re-anchor from.
//
// Columns start in table-layout:auto — the browser already knows how wide
// "checkout.example.com" needs to be, which beats guessing a `width` in a
// columns array and having it turn into "…". Once that first layout settles,
// its measured pixel widths are captured and locked in (table-layout:fixed),
// which is what makes a column's right edge draggable: fixed layout is the
// only one where an explicit width is authoritative rather than a hint. A
// column's own `width` still seeds the auto pass as a floor, so a
// deliberately narrow column (a checkbox, a short badge) doesn't get
// stretched by one long value elsewhere in it. Resizing is in-memory only —
// it resets with the table, same as every other page filter.
export function Table<T>({ columns, items, rowKey, rowClassName, onRowClick, emptyMessage, renderExpanded }: {
  columns: Column<T>[]
  items: T[]
  rowKey: (row: T) => string
  rowClassName?: (row: T) => string | undefined
  onRowClick?: (row: T) => void
  emptyMessage?: ReactNode
  renderExpanded?: (row: T) => ReactNode
}) {
  const tableRef = useRef<HTMLTableElement>(null)
  const [colWidths, setColWidths] = useState<number[] | null>(null)
  const dragState = useRef<{ i: number; startX: number; startW: number } | null>(null)

  useLayoutEffect(() => {
    if (colWidths || !tableRef.current) return
    const ths = tableRef.current.querySelectorAll('thead th')
    if (ths.length === 0) return
    setColWidths(Array.from(ths, (th) => th.getBoundingClientRect().width))
    // Re-checks after every items/columns change, but only actually measures
    // once (colWidths stays set from then on) — a filter narrowing the rows
    // shouldn't undo a resize the operator just made.
  }, [items, columns, colWidths])

  function startResize(i: number, e: React.MouseEvent) {
    if (!colWidths) return
    e.preventDefault()
    dragState.current = { i, startX: e.clientX, startW: colWidths[i] }
    const onMove = (ev: MouseEvent) => {
      const d = dragState.current
      if (!d) return
      const w = Math.max(40, d.startW + (ev.clientX - d.startX))
      setColWidths((cur) => (cur ? cur.map((cw, idx) => (idx === d.i ? w : cw)) : cur))
    }
    const onUp = () => {
      dragState.current = null
      window.removeEventListener('mousemove', onMove)
      window.removeEventListener('mouseup', onUp)
    }
    window.addEventListener('mousemove', onMove)
    window.addEventListener('mouseup', onUp)
  }

  if (items.length === 0) return <div className="m mus" style={{ padding: '12px 0' }}>{emptyMessage ?? 'Nothing to show.'}</div>
  return (
    <div className="tw">
      {/* Once locked to fixed layout, the table gets an explicit pixel width
          (the sum of its columns) rather than the base class's 100% — with a
          definite percentage width, a fixed-layout table rescales every
          column proportionally to keep summing to it, so dragging one column
          wider would visibly shrink the others. An explicit sum leaves
          nothing to redistribute: the table just grows, and .tw scrolls. */}
      <table
        ref={tableRef}
        className="t"
        style={colWidths ? { tableLayout: 'fixed', width: colWidths.reduce((a, b) => a + b, 0) } : { tableLayout: 'auto' }}
      >
        <thead>
          <tr>
            {columns.map((c, i) => (
              <th key={i} style={colWidths ? { width: colWidths[i] } : c.width ? { width: c.width } : undefined}>
                {c.name}
                {i < columns.length - 1 && <span className="col-rz" onMouseDown={(e) => startResize(i, e)} />}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {items.map((row, i) => {
            const zz = i % 2 === 1 ? 'zz' : ''
            const expanded = renderExpanded?.(row)
            return (
              <Fragment key={rowKey(row)}>
                <tr
                  className={[zz, rowClassName?.(row) ?? ''].filter(Boolean).join(' ') || undefined}
                  onClick={onRowClick && (() => onRowClick(row))}
                  style={onRowClick ? { cursor: 'pointer' } : undefined}
                >
                  {columns.map((c, j) => <td key={j}>{c.render(row)}</td>)}
                </tr>
                {expanded && (
                  <tr className={zz || undefined}>
                    <td className="ex" colSpan={columns.length}>{expanded}</td>
                  </tr>
                )}
              </Fragment>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}
