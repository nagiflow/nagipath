import { useQuery } from '@tanstack/react-query'
import { api, setCSRFToken } from '../client'
import { SessionResponseSchema } from '../pb/nagipath/api/v1/session_pb'

export function useSession() {
  return useQuery({
    queryKey: ['session'],
    queryFn: async () => {
      const session = await api.get('/session', SessionResponseSchema)
      setCSRFToken(session.csrfToken)
      return session
    },
    staleTime: 60_000,
  })
}
