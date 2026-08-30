import './Select.css'

export function Select({ options, value, onChange, small, disabled }: {
  options: { value: string; text: string }[]
  value: string
  onChange: (v: string) => void
  small?: boolean
  disabled?: boolean
}) {
  return (
    <span className="sel-wrap">
      <select className="sel" value={value} onChange={(e) => onChange(e.target.value)} disabled={disabled} style={small ? { fontSize: 11.5, padding: '3px 20px 3px 8px' } : undefined}>
        {options.map((o) => <option key={o.value} value={o.value}>{o.text}</option>)}
      </select>
    </span>
  )
}
