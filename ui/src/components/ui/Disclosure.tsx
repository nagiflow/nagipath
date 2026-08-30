import './Disclosure.css'

// The ▾/▸ an expandable row carries in its first column.
export function Disclosure({ open }: { open: boolean }) {
  return <span className="dis">{open ? '▾' : '▸'}</span>
}
