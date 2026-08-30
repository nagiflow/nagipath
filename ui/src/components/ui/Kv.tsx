import { Fragment, type ReactNode } from 'react'
import './Kv.css'

// design/'s .kv two-column grid — every detail panel on 7a/3c/7b/2o/2p uses
// it with a fixed label column and a free value column.
export function Kv({ rows, labelWidth = 88, mono = true }: {
  rows: [ReactNode, ReactNode][]
  labelWidth?: number
  mono?: boolean
}) {
  return (
    <div className={`kv${mono ? ' m' : ''}`} style={{ display: 'grid', gridTemplateColumns: `${labelWidth}px 1fr`, gap: '4px 8px' }}>
      {rows.map(([label, value], i) => (
        <Fragment key={i}>
          <span className="mus">{label}</span>
          <span>{value}</span>
        </Fragment>
      ))}
    </div>
  )
}
