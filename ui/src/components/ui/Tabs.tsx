import type { ReactNode } from 'react'
import './Tabs.css'

export function Tabs({ tabs, selected, onSelect }: { tabs: { id: string; label: ReactNode }[]; selected: string; onSelect: (id: string) => void }) {
  return (
    <div className="tabs">
      {tabs.map((t) => (
        <button key={t.id} className={`tab${t.id === selected ? ' on' : ''}`} onClick={() => onSelect(t.id)}>{t.label}</button>
      ))}
    </div>
  )
}
