import type { ReactNode } from 'react'
import { EuiProvider } from '@elastic/eui'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter } from 'react-router-dom'

// Custom theme tokens (ported from app.src.css's @theme block) land here in
// Phase 1, once a real screen exists to check them against.
const queryClient = new QueryClient()

export function AppProviders({ children }: { children: ReactNode }) {
  return (
    <EuiProvider colorMode="light">
      <QueryClientProvider client={queryClient}>
        <BrowserRouter>{children}</BrowserRouter>
      </QueryClientProvider>
    </EuiProvider>
  )
}
