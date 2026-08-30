import type { InputHTMLAttributes } from 'react'
import './Field.css'

export function Field({ placeholder, value, onChange, onEnter, grow, type, disabled }: {
  placeholder?: string
  value: string
  onChange: (v: string) => void
  onEnter?: () => void
  grow?: boolean
  type?: InputHTMLAttributes<HTMLInputElement>['type']
  disabled?: boolean
}) {
  return (
    <span className={`fld${grow ? ' f' : ''}`}>
      <input
        type={type ?? 'text'}
        placeholder={placeholder}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={(e) => e.key === 'Enter' && onEnter?.()}
        disabled={disabled}
      />
    </span>
  )
}
