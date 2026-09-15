import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../client'
import {
  CredentialsResponseSchema,
  HostKeysResponseSchema,
  MasterKeyResponseSchema,
  RetentionResponseSchema,
  CollectionDefaultsResponseSchema,
  UsersResponseSchema,
  AuditResponseSchema,
  ApiKeysResponseSchema,
  LicenseResponseSchema,
  DiagnosticsResponseSchema,
  CollectionsResponseSchema,
} from '../pb/nagipath/api/v1/settings_pb'

// ---------------------------------------------------------------- credentials

export function useCredentials(typeFilter: string) {
  const qs = typeFilter ? `?type=${encodeURIComponent(typeFilter)}` : ''
  return useQuery({
    queryKey: ['settings-credentials', typeFilter],
    queryFn: () => api.get(`/settings/credentials${qs}`, CredentialsResponseSchema),
  })
}

export function useAddCredential() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: Record<string, unknown>) => api.postAction('/settings/credentials', body),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['settings-credentials'] }),
  })
}

// ---------------------------------------------------------------- host keys

export function useHostKeys(state: string, cluster: string) {
  const qs = new URLSearchParams()
  if (state) qs.set('state', state)
  if (cluster) qs.set('cluster', cluster)
  const suffix = qs.toString()
  return useQuery({
    queryKey: ['settings-hostkeys', state, cluster],
    queryFn: () => api.get(`/settings/hostkeys${suffix ? `?${suffix}` : ''}`, HostKeysResponseSchema),
  })
}

export function useDecideHostKey() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, approve }: { id: number; approve: boolean }) =>
      api.postAction(`/hostkeys/${id}/decide`, { decision: approve ? 'approve' : 'reject' }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['settings-hostkeys'] }),
  })
}

// Off by default: a rekey (a key replacing an already-approved one) is never
// auto-approved by this setting, tofu_enabled or not — only a node's
// first-ever key is (internal/store/hostkey.go's CheckHostKey).
export function useSetHostKeyPolicy() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (tofuEnabled: boolean) => api.postAction('/settings/hostkeys/policy', { tofu_enabled: tofuEnabled }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['settings-hostkeys'] }),
  })
}

// ---------------------------------------------------------------- master key

export function useMasterKey() {
  return useQuery({
    queryKey: ['settings-masterkey'],
    queryFn: () => api.get('/settings/masterkey', MasterKeyResponseSchema),
  })
}

// ---------------------------------------------------------------- retention

export function useRetention() {
  return useQuery({
    queryKey: ['settings-retention'],
    queryFn: () => api.get('/settings/retention', RetentionResponseSchema),
  })
}

export function useSetRetention() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: Record<string, unknown>) => api.postAction('/settings/retention', body),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['settings-retention'] }),
  })
}

export function useRunRetention() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => api.postAction('/settings/retention/prune'),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['settings-retention'] }),
  })
}

// ---------------------------------------------------------------- collection defaults

export function useCollectionDefaults() {
  return useQuery({
    queryKey: ['settings-collection-defaults'],
    queryFn: () => api.get('/settings/collection-defaults', CollectionDefaultsResponseSchema),
  })
}

export function useSetCollectionDefaults() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: Record<string, unknown>) => api.postAction('/settings/collection-defaults', body),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['settings-collection-defaults'] }),
  })
}

// ---------------------------------------------------------------- users

export function useUsers() {
  return useQuery({
    queryKey: ['settings-users'],
    queryFn: () => api.get('/settings/users', UsersResponseSchema),
  })
}

export function useAddUser() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: Record<string, unknown>) => api.postAction('/settings/users', body),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['settings-users'] }),
  })
}

export function useSetUserDisabled() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, disabled }: { id: bigint; disabled: boolean }) =>
      api.postAction(`/settings/users/${id}/${disabled ? 'disable' : 'enable'}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['settings-users'] }),
  })
}

// ---------------------------------------------------------------- audit

export function useAudit(params: { actor: string; action: string; range: string; page: number; perPage?: number }) {
  const qs = new URLSearchParams()
  if (params.actor) qs.set('actor', params.actor)
  if (params.action) qs.set('action', params.action)
  if (params.range) qs.set('range', params.range)
  if (params.page > 1) qs.set('page', String(params.page))
  if (params.perPage) qs.set('per_page', String(params.perPage))
  return useQuery({
    queryKey: ['settings-audit', params.actor, params.action, params.range, params.page, params.perPage],
    queryFn: () => api.get(`/settings/audit?${qs.toString()}`, AuditResponseSchema),
  })
}

// ---------------------------------------------------------------- api keys

export function useAPIKeys(state: string) {
  const qs = state ? `?state=${encodeURIComponent(state)}` : ''
  return useQuery({
    queryKey: ['settings-apikeys', state],
    queryFn: () => api.get(`/settings/api-keys${qs}`, ApiKeysResponseSchema),
  })
}

export function useCreateAPIKey() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: { name: string; expiresDays: number }) => api.postAction('/settings/api-keys', body),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['settings-apikeys'] }),
  })
}

export function useRevokeAPIKey() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: bigint) => api.postAction(`/settings/api-keys/${id}/revoke`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['settings-apikeys'] }),
  })
}

// ---------------------------------------------------------------- license

export function useLicense() {
  return useQuery({
    queryKey: ['settings-license'],
    queryFn: () => api.get('/settings/license', LicenseResponseSchema),
  })
}

export function useInstallLicense() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (licenseText: string) => api.postAction('/settings/license', { licenseText }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['settings-license'] }),
  })
}

// ---------------------------------------------------------------- system

export function useDiagnostics(opts?: { enabled?: boolean }) {
  return useQuery({
    queryKey: ['settings-system'],
    queryFn: () => api.get('/settings/system', DiagnosticsResponseSchema),
    enabled: opts?.enabled,
  })
}

// ---------------------------------------------------------------- collection jobs

export function useCollections(params: { node: string; status: string; trigger: string; range: string; cursor: string }) {
  const qs = new URLSearchParams()
  if (params.node) qs.set('node', params.node)
  if (params.status) qs.set('status', params.status)
  if (params.trigger) qs.set('trigger', params.trigger)
  if (params.range) qs.set('range', params.range)
  if (params.cursor) qs.set('cursor', params.cursor)
  return useQuery({
    queryKey: ['collections', params.node, params.status, params.trigger, params.range, params.cursor],
    queryFn: () => api.get(`/collections?${qs.toString()}`, CollectionsResponseSchema),
  })
}
