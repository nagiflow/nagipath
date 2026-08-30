import type { ReactNode } from 'react'
import './Mono.css'

export function Mono({ subdued, subtle, children }: { subdued?: boolean; subtle?: boolean; children: ReactNode }) {
  return <span className={`m${subdued ? ' mu' : ''}${subtle ? ' mus' : ''}`}>{children}</span>
}
