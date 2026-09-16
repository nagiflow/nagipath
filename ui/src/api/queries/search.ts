import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { api } from '../client'
import { SearchResponseSchema } from '../pb/nagipath/api/v1/search_pb'

export function useSearch(params: {
  q: string; match: string; scope: string; page: number
  vendor: string[]; file: string[]; cluster: string[]; age: string[]
}) {
  const qs = new URLSearchParams()
  if (params.q) qs.set('q', params.q)
  if (params.match !== 'substring') qs.set('match', params.match)
  if (params.scope !== 'current') qs.set('scope', params.scope)
  if (params.page > 1) qs.set('page', String(params.page))
  for (const v of params.vendor) qs.append('vendor', v)
  for (const f of params.file) qs.append('file', f)
  for (const c of params.cluster) qs.append('cluster', c)
  for (const a of params.age) qs.append('age', a)
  return useQuery({
    queryKey: ['search', params.q, params.match, params.scope, params.page, params.vendor.join(','), params.file.join(','), params.cluster.join(','), params.age.join(',')],
    placeholderData: keepPreviousData,
    queryFn: () => api.get(`/search?${qs.toString()}`, SearchResponseSchema),
    enabled: !!params.q,
  })
}
