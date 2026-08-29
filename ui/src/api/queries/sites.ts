import { useQuery } from '@tanstack/react-query'
import { api } from '../client'
import { SiteDetailResponseSchema, SitesListResponseSchema } from '../pb/nagipath/api/v1/sites_pb'

export function useSites(params: { q: string; site?: string }) {
  const qs = new URLSearchParams()
  if (params.q) qs.set('q', params.q)
  if (params.site) qs.set('site', params.site)
  return useQuery({
    queryKey: ['sites', params.q, params.site ?? ''],
    queryFn: () => api.get(`/sites?${qs.toString()}`, SitesListResponseSchema),
  })
}

export function useSite(name: string, params: { tab: string; variant?: string }) {
  const qs = new URLSearchParams()
  if (params.tab !== 'overview') qs.set('tab', params.tab)
  if (params.variant) qs.set('variant', params.variant)
  const suffix = qs.toString()
  return useQuery({
    queryKey: ['site', name, params.tab, params.variant ?? ''],
    queryFn: () => api.get(`/sites/${encodeURIComponent(name)}${suffix ? `?${suffix}` : ''}`, SiteDetailResponseSchema),
    enabled: !!name,
  })
}
