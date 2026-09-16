import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { api } from '../client'
import { SnapshotFileResponseSchema, SnapshotsListResponseSchema } from '../pb/nagipath/api/v1/snapshots_pb'

export function useSnapshots(params: { q?: string; range?: string; cursor?: string }) {
  const qs = new URLSearchParams()
  if (params.q) qs.set('q', params.q)
  if (params.range) qs.set('range', params.range)
  if (params.cursor) qs.set('cursor', params.cursor)
  return useQuery({
    queryKey: ['snapshots', params.q ?? '', params.range ?? '', params.cursor ?? ''],
    placeholderData: keepPreviousData,
    queryFn: () => api.get(`/snapshots?${qs.toString()}`, SnapshotsListResponseSchema),
  })
}

export function useSnapshotFile(snapshotID: number, fileID: number, byteOffset?: number) {
  const qs = byteOffset !== undefined ? `?b=${byteOffset}` : ''
  return useQuery({
    queryKey: ['snapshot-file', snapshotID, fileID, byteOffset ?? 0],
    placeholderData: keepPreviousData,
    queryFn: () => api.get(`/snapshots/${snapshotID}/file/${fileID}${qs}`, SnapshotFileResponseSchema),
    enabled: !!snapshotID && !!fileID,
  })
}
