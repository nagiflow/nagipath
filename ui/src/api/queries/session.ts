import { useQuery } from '@tanstack/react-query'
import { api, setCSRFToken } from '../client'
import type { SessionResponse } from '../types'

export function useSession() {
  return useQuery({
    queryKey: ['session'],
    queryFn: async () => {
      const session = await api.get<SessionResponse>('/session')
      setCSRFToken(session.csrf_token)
      return session
    },
    staleTime: 60_000,
  })
}
