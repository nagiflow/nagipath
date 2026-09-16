import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../client'
import { ClustersResponseSchema } from '../pb/nagipath/api/v1/clusters_pb'

export function useClusters(params: { q: string; drift: string; sort: string; cluster?: number }) {
  const qs = new URLSearchParams()
  if (params.q) qs.set('q', params.q)
  if (params.drift !== 'any') qs.set('drift', params.drift)
  if (params.sort !== 'drift') qs.set('sort', params.sort)
  if (params.cluster) qs.set('cluster', String(params.cluster))

  return useQuery({
    queryKey: ['clusters', params.q, params.drift, params.sort, params.cluster ?? 0],
    placeholderData: keepPreviousData,
    queryFn: () => api.get(`/clusters?${qs.toString()}`, ClustersResponseSchema),
  })
}

export function useRenameCluster() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (body: { cluster: number; name: string }) => api.postAction('/clusters/rename', body),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['clusters'] }),
  })
}

// useSetGoldenPeer declares a cluster's golden peer (internal/api/drift.go's
// postDriftGolden) — omitting `instance` clears it. Used by NodesListPage's
// "Clear baseline" and DriftPage's "Set/Change baseline" picker; both read
// through GetDrift or ListClusters, so both need invalidating.
export function useSetGoldenPeer() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (body: { cluster: number; instance?: number }) => api.postAction('/drift/golden', body),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['clusters'] })
      queryClient.invalidateQueries({ queryKey: ['drift'] })
    },
  })
}
