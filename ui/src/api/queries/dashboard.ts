import { useQuery } from '@tanstack/react-query'
import { api } from '../client'
import type { DashboardResponse } from '../types'

export function useDashboard(params: { cluster?: number; severity?: string; attentionCluster?: number }) {
  const q = new URLSearchParams()
  if (params.cluster) q.set('cluster', String(params.cluster))
  if (params.severity) q.set('severity', params.severity)
  if (params.attentionCluster) q.set('attention_cluster', String(params.attentionCluster))
  const qs = q.toString()

  return useQuery({
    queryKey: ['dashboard', params.cluster ?? 0, params.severity ?? 'all', params.attentionCluster ?? 0],
    queryFn: () => api.get<DashboardResponse>(`/dashboard${qs ? `?${qs}` : ''}`),
  })
}
