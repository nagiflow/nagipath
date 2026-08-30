import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../client'
import {
  ImportNodesResponseSchema,
  NodeDetailResponseSchema,
  NodesListResponseSchema,
  TestConnectionResponseSchema,
} from '../pb/nagipath/api/v1/nodes_pb'

export function useNodes(q: string) {
  const qs = new URLSearchParams()
  if (q) qs.set('q', q)
  return useQuery({
    queryKey: ['nodes', q],
    queryFn: () => api.get(`/nodes?${qs.toString()}`, NodesListResponseSchema),
  })
}

export function useNode(id: number, params: { tab: string; process?: number; pool?: number; file?: number; site?: string; route?: string }) {
  const qs = new URLSearchParams()
  if (params.process) qs.set('process', String(params.process))
  if (params.pool) qs.set('pool', String(params.pool))
  if (params.file) qs.set('file', String(params.file))
  if (params.site) qs.set('site', params.site)
  if (params.route) qs.set('route', params.route)
  const suffix = qs.toString()
  const tabPath = params.tab ? `/${params.tab}` : ''
  return useQuery({
    queryKey: ['node', id, params.tab, params.process ?? 0, params.pool ?? 0, params.file ?? 0, params.site ?? '', params.route ?? ''],
    queryFn: () => api.get(`/nodes/${id}${tabPath}${suffix ? `?${suffix}` : ''}`, NodeDetailResponseSchema),
    enabled: !!id,
    // The running-collection banner self-terminates the same way the old
    // htmx `hx-trigger="every 3s"` poll did (node.html) — refetch while a
    // collection is in flight, stop the moment it isn't.
    refetchInterval: (query) => (query.state.data?.running ? 3000 : false),
  })
}

export function useImportNodes() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (body: { inventory: string; credentialId: number }) =>
      api.post('/nodes/import', ImportNodesResponseSchema, body),
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

// One dial-and-Check(), the same connection the collector opens with but
// without collecting anything — Import inventory step 2's per-row "Test" and
// "Retry", and a node's own Retry once a host key is approved.
export function useTestNodeConnection(id: number) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: () => api.post(`/nodes/${id}/test`, TestConnectionResponseSchema),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['nodes'] }),
  })
}

// Import inventory step 3's "start collection now": the same per-node
// /collect this file's useCollectNode already wraps, fired for every node
// that tested ready, in one settle rather than a hook instance per row.
export function useCollectNodes() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (ids: number[]) => Promise.allSettled(ids.map((id) => api.postAction(`/nodes/${id}/collect`))),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['nodes'] }),
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
