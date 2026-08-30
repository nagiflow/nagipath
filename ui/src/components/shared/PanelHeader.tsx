import type { ReactNode } from 'react'
import { PanelHeader as UiPanelHeader } from '../ui'

// Thin wrapper — see PanelHeader (components/ui/PanelHeader.tsx) for the actual
// .phd/.ph2 rendering.
export function PanelHeader(props: { title: ReactNode; meta?: ReactNode; actions?: ReactNode }) {
  return <UiPanelHeader {...props} />
}
