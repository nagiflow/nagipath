import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../client'
import { TraceResponseSchema, ProbeRunPBSchema } from '../pb/nagipath/api/v1/trace_pb'
import { ProbeHistoryResponseSchema, ProbeDetailResponseSchema } from '../pb/nagipath/api/v1/probe_pb'

export function useTrace(params: { url: string; scheme: string; hostname: string; path: string; port: string; method: string; run: string; prov: string }) {
  const qs = new URLSearchParams()
  if (params.url) qs.set('url', params.url)
  if (params.scheme) qs.set('scheme', params.scheme)
  if (params.hostname) qs.set('hostname', params.hostname)
  if (params.path) qs.set('path', params.path)
  if (params.port) qs.set('port', params.port)
  if (params.method) qs.set('method', params.method)
  if (params.run) qs.set('run', params.run)
  if (params.prov) qs.set('prov', params.prov)
  return useQuery({
    queryKey: ['trace', qs.toString()],
    queryFn: () => api.get(`/trace?${qs.toString()}`, TraceResponseSchema),
  })
}

export function usePostTrace() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: { url?: string; scheme?: string; hostname?: string; path?: string; port?: number; method?: string }) =>
      api.post('/trace', TraceResponseSchema, body),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['trace'] }),
  })
}

export function useStartProbe() {
  return useMutation({
    mutationFn: (body: { url: string; method: string; trace?: number }) => api.postAction('/trace/probe', body),
  })
}

// useProbeRun polls a live run every 2 seconds until Done — the same
// cadence internal/web's old htmx page used (docs/adr/0017: a boolean poll
// is already the right mechanism for this).
export function useProbeRun(runId: string) {
  return useQuery({
    queryKey: ['probe-run', runId],
    queryFn: () => api.get(`/trace/run/${runId}`, ProbeRunPBSchema),
    enabled: !!runId,
    refetchInterval: (query) => (query.state.data?.done ? false : 2000),
  })
}

export function useProbeHistory(params: { url: string; q: string; range: string; actor: string; outcome: string; probe: string; cursor: string }) {
  const qs = new URLSearchParams()
  if (params.url) qs.set('url', params.url)
  if (params.q) qs.set('q', params.q)
  if (params.range) qs.set('range', params.range)
  if (params.actor) qs.set('actor', params.actor)
  if (params.outcome) qs.set('outcome', params.outcome)
  if (params.probe) qs.set('probe', params.probe)
  if (params.cursor) qs.set('cursor', params.cursor)
  return useQuery({
    queryKey: ['probe-history', qs.toString()],
    placeholderData: keepPreviousData,
    queryFn: () => api.get(`/trace/history?${qs.toString()}`, ProbeHistoryResponseSchema),
  })
}

export function useProbeDetail(id: string) {
  return useQuery({
    queryKey: ['probe-detail', id],
    queryFn: () => api.get(`/trace/probe/${id}`, ProbeDetailResponseSchema),
    enabled: !!id,
  })
}
