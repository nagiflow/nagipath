import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../client'
import { DriftResponseSchema, DriftReviewResponseSchema } from '../pb/nagipath/api/v1/drift_pb'

export function useDrift(params: { cluster: string; baseline?: string; scope?: string }) {
  const qs = new URLSearchParams()
  if (params.cluster) qs.set('cluster', params.cluster)
  if (params.baseline) qs.set('baseline', params.baseline)
  if (params.scope) qs.set('scope', params.scope)
  return useQuery({
    queryKey: ['drift', params.cluster, params.baseline ?? '', params.scope ?? ''],
    placeholderData: keepPreviousData,
    queryFn: () => api.get(`/drift?${qs.toString()}`, DriftResponseSchema),
  })
}

export function useDriftReview(instanceID: number) {
  return useQuery({
    queryKey: ['drift-review', instanceID],
    queryFn: () => api.get(`/drift/review/${instanceID}`, DriftReviewResponseSchema),
    enabled: !!instanceID,
  })
}

export function useRecomputeDrift() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (body: { cluster: string; baseline?: string }) => api.postAction('/drift/recompute', body),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['drift'] }),
  })
}

export function useIgnoreFinding() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (body: { cluster: number; object_kind: string; field: string; pattern: string; reason: string }) =>
      api.postAction('/drift/ignore', body),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['drift'] }),
  })
}

export function useUnignore() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => api.postAction(`/drift/ignore/${id}/delete`),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['drift'] }),
  })
}
