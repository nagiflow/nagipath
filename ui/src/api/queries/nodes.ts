import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../client'
import { NodeDetailResponseSchema, NodesListResponseSchema } from '../pb/nagipath/api/v1/nodes_pb'

export function useNodes(q: string) {
  const qs = new URLSearchParams()
  if (q) qs.set('q', q)
  return useQuery({
    queryKey: ['nodes', q],
    queryFn: () => api.get(`/nodes?${qs.toString()}`, NodesListResponseSchema),
  })
}

export function useNode(id: number, params: { tab: string; process?: number; file?: number }) {
  const qs = new URLSearchParams()
  if (params.process) qs.set('process', String(params.process))
  if (params.file) qs.set('file', String(params.file))
  const suffix = qs.toString()
  const tabPath = params.tab ? `/${params.tab}` : ''
  return useQuery({
    queryKey: ['node', id, params.tab, params.process ?? 0, params.file ?? 0],
    queryFn: () => api.get(`/nodes/${id}${tabPath}${suffix ? `?${suffix}` : ''}`, NodeDetailResponseSchema),
    enabled: !!id,
    // The running-collection banner self-terminates the same way the old
    // htmx `hx-trigger="every 3s"` poll did (node.html) — refetch while a
    // collection is in flight, stop the moment it isn't.
    refetchInterval: (query) => (query.state.data?.running ? 3000 : false),
  })
}

export function useAddNode() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (body: { address: string; port?: number; display_name?: string; username?: string; credential_id?: number }) =>
      api.postAction('/nodes', body),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['nodes'] }),
  })
}

export function useCollectNode(id: number) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: () => api.postAction(`/nodes/${id}/collect`),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['node', id] }),
  })
}

export function useDeleteNode() {
  return useMutation({
    mutationFn: (id: number) => api.postAction(`/nodes/${id}/delete`),
  })
}

export function useChangeNodeCredential(id: number) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (credential_id: number) => api.postAction(`/nodes/${id}/credential`, { credential_id }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['node', id] }),
  })
}

export function useDecideHostKey() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, decision }: { id: number; decision: 'approve' | 'reject' }) =>
      api.postAction(`/hostkeys/${id}/decide`, { decision }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['node'] }),
  })
}
