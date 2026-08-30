import type { ReactNode } from 'react'
import './Checkbox.css'

export function Checkbox({ checked, onChange, label, id }: { checked: boolean; onChange: () => void; label?: ReactNode; id?: string }) {
  const box = <input type="checkbox" className="cb" checked={checked} onChange={onChange} id={id} />
  if (!label) return box
  return (
    <label htmlFor={id} style={{ display: 'flex', alignItems: 'center', gap: 7, cursor: 'pointer', fontSize: 12.5 }}>
      {box}{label}
    </label>
  )
}
