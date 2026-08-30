import type { ReactNode } from 'react'
import './Radio.css'

export function Radio({ checked, onChange, label, name }: { checked: boolean; onChange: () => void; label?: ReactNode; name?: string }) {
  return (
    <label style={{ display: 'flex', alignItems: 'center', gap: 7, cursor: 'pointer', fontSize: 12.5 }}>
      <input type="radio" className="rd" checked={checked} onChange={onChange} name={name} />
      {label}
    </label>
  )
}
