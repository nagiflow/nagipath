import './Bar.css'

export function Bar({ pct }: { pct: number }) {
  return <div className="bar"><i style={{ width: `${Math.max(0, Math.min(100, pct))}%` }} /></div>
}
