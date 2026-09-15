import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../client'
import {
  ImportNodesResponseSchema,
  NodeFileResponseSchema,
  RestartInstanceResponseSchema,
  NodeDetailResponseSchema,
  NodesListResponseSchema,
  TestConnectionResponseSchema,
  WriteNodeFileResponseSchema,
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
    mutationFn: (body: { inventory: string; credentialId: number; bastionNodeId?: number }) =>
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

// bastion_node_id 0 clears it back to a direct connection.
export function useChangeNodeBastion(id: number) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (bastion_node_id: number) => api.postAction(`/nodes/${id}/bastion`, { bastion_node_id }),
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

// The live file on the node, not the snapshot's copy of it — an edit has to
// start from what is there now or it quietly reverts anything changed since
// the last collection. Never cached: two seconds later it may not be true.
export function useNodeFile(nodeID: number, path: string) {
  return useQuery({
    queryKey: ['node-file', nodeID, path],
    queryFn: () => api.get(`/nodes/${nodeID}/livefile?path=${encodeURIComponent(path)}`, NodeFileResponseSchema),
    enabled: !!nodeID && !!path,
    gcTime: 0,
    staleTime: 0,
  })
}

export function useWriteNodeFile(nodeID: number) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (body: { path: string; body: string }) =>
      api.post(`/nodes/${nodeID}/livefile`, WriteNodeFileResponseSchema, body),
    // The node's config changed under the last collection, so what the page is
    // showing is now history — refetch both the node and the file.
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['node'] }),
  })
}

// An empty command means "use the one the server resolved"; anything else is
// stored as that process's restart command and prefilled next time.
export function useRestartInstance(instanceID: number) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (command: string) =>
      api.post(`/instances/${instanceID}/restart`, RestartInstanceResponseSchema, { command }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['node'] }),
  })
}
