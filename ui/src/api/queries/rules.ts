import { useQuery } from '@tanstack/react-query'
import { api } from '../client'
import { RulesResponseSchema } from '../pb/nagipath/api/v1/rules_pb'

export function useRules(params: { url: string; hostname: string; path: string; scheme: string; port: string; classes: string[]; vendors: string[] }) {
  const qs = new URLSearchParams()
  if (params.url) qs.set('url', params.url)
  if (params.hostname) qs.set('hostname', params.hostname)
  if (params.path) qs.set('path', params.path)
  if (params.scheme) qs.set('scheme', params.scheme)
  if (params.port) qs.set('port', params.port)
  for (const c of params.classes) qs.append('class', c)
  for (const v of params.vendors) qs.append('vendor', v)
  return useQuery({
    queryKey: ['rules', params.url, params.hostname, params.path, params.scheme, params.port, params.classes.join(','), params.vendors.join(',')],
    queryFn: () => api.get(`/rules?${qs.toString()}`, RulesResponseSchema),
  })
}
