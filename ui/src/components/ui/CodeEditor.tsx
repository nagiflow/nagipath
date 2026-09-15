import './CodeEditor.css'

// A plain textarea, deliberately: syntax highlighting in an editable field
// needs a mirrored overlay, and this is for a targeted change to a config file
// that is read in CodeBlock and validated by the vendor on restart — not an IDE.
export function CodeEditor({ value, onChange, disabled }: {
  value: string
  onChange: (v: string) => void
  disabled?: boolean
}) {
  return (
    <div className="ced">
      <textarea value={value} onChange={(e) => onChange(e.target.value)} disabled={disabled} spellCheck={false} />
    </div>
  )
}
