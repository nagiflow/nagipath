import type { ReactNode } from 'react'
import { Panel } from '../../components/ui'

// The signed-out card layout Login and Setup share — ports login.html's and
// setup.html's bare <section class="solo">, which layout.html gave a plain
// <main> with no header or sidebar since there is no session yet to draw
// either from. design/'s screens 9a (Sign in) and 9b (First-run setup) are this
// card: the whole canvas, one 380px panel, no chrome.
export function AuthCard({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div style={{ minHeight: '100vh', display: 'flex', alignItems: 'center', justifyContent: 'center', background: '#fafbfd' }}>
      <Panel style={{ width: 380 }}>
        <h1 className="h1" style={{ marginBottom: 14 }}>{title}</h1>
        {children}
      </Panel>
    </div>
  )
}
