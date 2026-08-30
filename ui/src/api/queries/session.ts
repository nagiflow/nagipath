import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { MessageShape } from '@bufbuild/protobuf'
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

// Login, Setup and password-change (auth.go's postLogin/postSetup/
// postPassword) all respond with a fresh SessionResponse after establishing
// a new session, rather than the bare {ok:true} envelope most mutations use
// — it saves the client a second round trip for exactly the state it's about
// to need (CSRF token, must-change-password flag). applySession writes that
// response straight into the ['session'] cache instead of refetching.
function useApplySession() {
  const qc = useQueryClient()
  return (session: MessageShape<typeof SessionResponseSchema>) => {
    setCSRFToken(session.csrfToken)
    qc.setQueryData(['session'], session)
  }
}

export function useLogin() {
  const applySession = useApplySession()
  return useMutation({
    mutationFn: (body: { username: string; password: string }) => api.postPublic('/login', SessionResponseSchema, body),
    onSuccess: applySession,
  })
}

export function useSetup() {
  const applySession = useApplySession()
  return useMutation({
    mutationFn: (body: { username: string; password: string; confirm: string }) =>
      api.postPublic('/setup', SessionResponseSchema, body),
    onSuccess: applySession,
  })
}

export function useChangePassword() {
  const applySession = useApplySession()
  return useMutation({
    mutationFn: (body: { current?: string; new: string; confirm: string }) =>
      api.post('/password', SessionResponseSchema, body),
    onSuccess: applySession,
  })
}
