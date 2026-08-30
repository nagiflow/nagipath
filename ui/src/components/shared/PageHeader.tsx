import type { ReactNode } from 'react'
import { PageHeader as UiPageHeader } from '../ui'

// Thin wrapper so the 31 pages that already call <PageHeader> didn't need
// touching when the app moved off EUI onto the design's literal markup — see
// PageHeader (components/ui/PageHeader.tsx) for the actual .ptitle/.h1 rendering.
export function PageHeader(props: { title: ReactNode; badge?: ReactNode; meta?: ReactNode; actions?: ReactNode }) {
  return <UiPageHeader {...props} />
}
