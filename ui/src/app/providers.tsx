import type { ReactNode } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter } from 'react-router-dom'
import { APIError } from '../api/client'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // A 4xx is a definitive answer (not found, forbidden, bad input) — retrying
      // it just delays showing that answer. Only retry what a retry can fix.
      retry: (failureCount, error) => {
        if (error instanceof APIError && error.status >= 400 && error.status < 500) return false
        return failureCount < 3
      },
    },
  },
})

export function AppProviders({ children }: { children: ReactNode }) {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>{children}</BrowserRouter>
    </QueryClientProvider>
  )
}
