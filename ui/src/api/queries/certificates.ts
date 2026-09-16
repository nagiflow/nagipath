import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { api } from '../client'
import { CertificateDetailResponseSchema, CertificatesListResponseSchema } from '../pb/nagipath/api/v1/certificates_pb'

export function useCertificates(params: { expires?: string; issuer?: string; cluster?: string; includeCAs?: boolean }) {
  const qs = new URLSearchParams()
  if (params.expires) qs.set('expires', params.expires)
  if (params.issuer) qs.set('issuer', params.issuer)
  if (params.cluster) qs.set('cluster', params.cluster)
  if (params.includeCAs) qs.set('include_cas', '1')
  return useQuery({
    queryKey: ['certificates', params.expires ?? '', params.issuer ?? '', params.cluster ?? '', params.includeCAs ?? false],
    placeholderData: keepPreviousData,
    queryFn: () => api.get(`/certificates?${qs.toString()}`, CertificatesListResponseSchema),
  })
}

export function useCertificate(id: number, tab: string) {
  const qs = tab !== 'overview' ? `?tab=${tab}` : ''
  return useQuery({
    queryKey: ['certificate', id, tab],
    placeholderData: keepPreviousData,
    queryFn: () => api.get(`/certificates/${id}${qs}`, CertificateDetailResponseSchema),
    enabled: !!id,
  })
}
