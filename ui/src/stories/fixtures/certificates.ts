import { CertificateDetailResponseSchema, CertificatesListResponseSchema } from '../../api/pb/nagipath/api/v1/certificates_pb'
import { pb } from '../support/mockApi'

export const certificatesListFixture = pb(CertificatesListResponseSchema, {
  expires: '30d',
  issuer: '',
  cluster: '',
  summary: '41 of 612 certificates expire within 30 days · 63 bindings affected',
  issuers: ['CN=Example Internal CA G2', "CN=Let's Encrypt R11", 'CN=DigiCert TLS RSA SHA256 2020 CA1'],
  clusters: [
    { id: 1n, name: 'legacy', members: 24 },
    { id: 2n, name: 'app-iad3', members: 312 },
    { id: 3n, name: 'app-sfo2', members: 74 },
  ],
  list: [
    { id: 701n, fingerprint: 'SHA256:a1b…9f0', subjectCn: 'checkout.example.com', issuerDn: 'CN=Example Internal CA G2', notBefore: '2025-09-08T00:00:00Z', notAfter: '2026-09-08T00:00:00Z', bindings: 12, hosts: 'app-iad3 (8), app-sfo2 (4)', serves: ['checkout.example.com', 'checkout-int.example.com'], sans: ['checkout.example.com', 'checkout-int.example.com'], keyAlgorithm: 'ECDSA', keyBits: 256 },
    { id: 702n, fingerprint: 'SHA256:b2c…7d1', subjectCn: '*.cdn.example.net', issuerDn: "CN=Let's Encrypt R11", notBefore: '2026-06-16T00:00:00Z', notAfter: '2026-09-14T00:00:00Z', bindings: 31, hosts: 'edge-iad3 (12), edge-sfo2 (6)', serves: ['static.cdn.example.net', 'img.cdn.example.net'], sans: ['*.cdn.example.net'], keyAlgorithm: 'RSA', keyBits: 2048 },
    { id: 703n, fingerprint: 'SHA256:c3d…5e2', subjectCn: 'legacy-admin.example.com', issuerDn: 'CN=Example Internal CA G2', notBefore: '2025-09-21T00:00:00Z', notAfter: '2026-09-21T00:00:00Z', bindings: 2, hosts: 'legacy (2)', serves: ['legacy-admin.example.com'], sans: ['legacy-admin.example.com'], keyAlgorithm: 'RSA', keyBits: 4096 },
    { id: 704n, fingerprint: 'SHA256:d4e…3f3', subjectCn: 'Example Internal CA G2', issuerDn: 'CN=Example Root CA', notBefore: '2023-01-01T00:00:00Z', notAfter: '2033-01-01T00:00:00Z', isCa: true, bindings: 0, hosts: '—', serves: [], sans: [], keyAlgorithm: 'RSA', keyBits: 4096 },
  ],
})

export const certificateDetailFixture = pb(CertificateDetailResponseSchema, {
  activeTab: 'bindings',
  bindingCount: 12,
  fileCount: 3,
  cert: {
    id: 701n,
    fingerprint: 'SHA256:a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90',
    subjectCn: 'checkout.example.com',
    subjectDn: 'CN=checkout.example.com,O=Example Inc,C=US',
    sans: ['checkout.example.com', 'checkout-int.example.com'],
    issuerDn: 'CN=Example Internal CA G2,O=Example Inc,C=US',
    serial: '0a:44:9f:11:03:7c:52:e1',
    notBefore: '2025-09-08T00:00:00Z',
    notAfter: '2026-09-08T00:00:00Z',
    keyAlgorithm: 'ECDSA',
    keyBits: 256,
    sigAlgorithm: 'ECDSA-SHA256',
    isCa: false,
    firstSeen: '2025-09-09T04:00:00Z',
    lastSeen: '2026-08-30T09:35:00Z',
  },
  bindings: [
    { instanceId: 900n, nodeId: 41n, instance: 'nginx (:443)', node: 'app-iad3-17', clusterName: 'app-iad3', snapshotId: 8801n, fileId: 9102n, filePath: '/etc/ssl/checkout.pem', siteNames: 'checkout.example.com', port: 443, combinedPem: true },
    { instanceId: 800n, nodeId: 12n, instance: 'nginx (:443)', node: 'app-iad3-01', clusterName: 'app-iad3', snapshotId: 8790n, fileId: 9002n, filePath: '/etc/ssl/checkout.pem', siteNames: 'checkout.example.com', port: 443, combinedPem: true },
    { instanceId: 640n, nodeId: 88n, instance: 'haproxy (:443)', node: 'app-sfo2-04', clusterName: 'app-sfo2', snapshotId: 8712n, fileId: 8802n, filePath: '/etc/haproxy/certs/checkout.pem', siteNames: 'checkout.example.com', port: 443, combinedPem: true },
  ],
  filePaths: [
    { path: '/etc/ssl/checkout.pem', nodeCount: 8, bundleType: 'combined (leaf + key)' },
    { path: '/etc/haproxy/certs/checkout.pem', nodeCount: 4, bundleType: 'combined (leaf + key)' },
    { path: '/etc/ssl/chain/checkout-chain.pem', nodeCount: 8, bundleType: 'chain only' },
  ],
})
