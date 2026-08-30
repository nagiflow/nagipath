import {
  ApiKeysResponseSchema, AuditResponseSchema, CollectionDefaultsResponseSchema, CollectionsResponseSchema,
  CredentialsResponseSchema, DiagnosticsResponseSchema, HostKeysResponseSchema, LicenseResponseSchema,
  MasterKeyResponseSchema, RetentionResponseSchema, UsersResponseSchema,
} from '../../api/pb/nagipath/api/v1/settings_pb'
import { pb } from '../support/mockApi'

const credentials = [
  { id: 2n, name: 'iad3-collector', username: 'nagipath', authKind: 'ssh_key', fingerprint: 'SHA256:9mQ…v1c', createdAt: '2026-05-01T08:00:00Z', nodeCount: 386, lastUsed: '2026-08-30T09:38:00Z' },
  { id: 3n, name: 'legacy-password', username: 'root', authKind: 'password', createdAt: '2026-06-11T08:00:00Z', nodeCount: 24, lastUsed: '2026-08-30T09:40:00Z' },
  { id: 4n, name: 'vault-sfo2', username: 'nagipath', authKind: 'external', externalRef: 'vault://secret/nagipath/sfo2', createdAt: '2026-07-02T08:00:00Z', nodeCount: 18, lastUsed: '2026-08-30T09:20:00Z' },
]

export const credentialsFixture = pb(CredentialsResponseSchema, {
  credentials,
  authKinds: ['ssh_key', 'password', 'external'],
})

export const hostKeysFixture = pb(HostKeysResponseSchema, {
  stats: { pending: 3, changed: 1, approved: 425, algorithms: { 'ssh-ed25519': 402, 'ecdsa-sha2-nistp256': 26 } },
  clusters: ['legacy', 'app-iad3', 'app-sfo2', 'edge-iad3', 'edge-sfo2'],
  keys: [
    { nodeName: 'lb-legacy-04', nodeAddress: '10.0.4.9', cluster: 'legacy', previous: 'SHA256:oldQ…7hA', key: { id: 90n, nodeId: 104n, algorithm: 'ssh-ed25519', fingerprint: 'SHA256:2Fq…8kZ', state: 'changed', firstSeenAt: '2026-08-30T04:12:00Z' } },
    { nodeName: 'edge-iad3-07', nodeAddress: '10.7.1.7', cluster: 'edge-iad3', key: { id: 91n, nodeId: 431n, algorithm: 'ssh-ed25519', fingerprint: 'SHA256:7Lp…3vT', state: 'pending', firstSeenAt: '2026-08-30T08:55:00Z' } },
    { nodeName: 'edge-iad3-08', nodeAddress: '10.7.1.8', cluster: 'edge-iad3', key: { id: 92n, nodeId: 432n, algorithm: 'ecdsa-sha2-nistp256', fingerprint: 'SHA256:1Zx…9qM', state: 'pending', firstSeenAt: '2026-08-30T08:55:00Z' } },
  ],
})

export const masterKeyFixture = pb(MasterKeyResponseSchema, {
  path: '/var/lib/nagipath/master.key',
  dataDir: '/var/lib/nagipath',
  credentialsEncrypted: 3,
  apiKeysEncrypted: 4,
  jobLogsEncrypted: 18422,
})

export const retentionFixture = pb(RetentionResponseSchema, {
  snapshotDays: 90,
  minPerInstance: 3,
  jobLogDays: 30,
  probeDays: 30,
  auditDays: 400,
  stats: {
    indexSizeBytes: 4_294_967_296n,
    snapshots: 18422,
    jobLogs: 41230,
    traces: 9012,
    lastPrune: {
      ranAt: '2026-08-30T03:00:00Z',
      durationSeconds: 12.4,
      examined: { snapshots: 2400, jobLogs: 5100, traces: 900, auditEvents: 12000 },
      deleted: { snapshots: 1800n, jobLogs: 4200n, probes: 600n, auditEvents: 0n, blobs: 1240n },
      kept: { snapshots: 600, jobLogs: 900, traces: 300, auditEvents: 12000 },
      freedBytes: 734_003_200n,
    },
  },
})

export const collectionDefaultsFixture = pb(CollectionDefaultsResponseSchema, {
  credentials,
  defaultCredential: 2n,
  intervalMinutes: 60,
  jitterSeconds: 300,
  sshWorkers: 16,
  commandTimeout: 30,
  maxFiles: 500,
  probeRedirects: 5,
  probeLookback: 120,
})

export const usersFixture = pb(UsersResponseSchema, {
  users: [
    { id: 1n, username: 'awong', role: 'admin', displayName: 'Allen Wong', createdAt: '2026-05-01T08:00:00Z', lastLoginAt: '2026-08-30T08:00:00Z' },
    { id: 7n, username: 'rlee', role: 'viewer', displayName: 'R. Lee', createdAt: '2026-06-02T08:00:00Z', lastLoginAt: '2026-08-29T14:00:00Z' },
    { id: 9n, username: 'newadmin', role: 'admin', mustChangePassword: true, createdAt: '2026-08-29T09:00:00Z' },
    { id: 11n, username: 'contractor', role: 'viewer', disabled: true, createdAt: '2026-07-19T09:00:00Z', lastLoginAt: '2026-08-01T10:00:00Z' },
  ],
})

export const apiKeysFixture = pb(ApiKeysResponseSchema, {
  keys: [
    { id: 40n, name: 'ci-deploy', username: 'api:ci-deploy', prefix: 'np_7f3c', createdAt: '2026-06-01T08:00:00Z', lastUsedAt: '2026-08-30T09:00:00Z', expiresAt: '2026-12-01T00:00:00Z' },
    { id: 41n, name: 'grafana-readonly', username: 'api:grafana', prefix: 'np_2b91', createdAt: '2026-07-11T08:00:00Z', lastUsedAt: '2026-08-30T09:39:00Z' },
    { id: 42n, name: 'old-laptop', username: 'api:awong', prefix: 'np_9d02', createdAt: '2026-05-20T08:00:00Z', revokedAt: '2026-08-02T11:00:00Z' },
  ],
})

export const auditFixture = pb(AuditResponseSchema, {
  total: 12040,
  totalPages: 241,
  page: 1,
  perPage: 50,
  actions: ['login', 'login_failed', 'node.collect', 'credential.create', 'hostkey.decide', 'license.install', 'api_key.revoke'],
  events: [
    { at: '2026-08-30T09:42:00Z', actorLabel: 'awong', action: 'probe.start', targetKind: 'trace', targetLabel: 'https://checkout.example.com/api/v2/cart', outcome: 'ok', sourceIp: '10.7.1.1' },
    { at: '2026-08-30T09:40:00Z', actorLabel: 'api:ci-deploy', action: 'node.collect', targetKind: 'node', targetLabel: 'lb-legacy-04', outcome: 'failed', detail: 'ssh: handshake failed — host key changed', sourceIp: '10.4.0.20' },
    { at: '2026-08-30T08:55:00Z', actorLabel: 'awong', action: 'hostkey.decide', targetKind: 'hostkey', targetLabel: 'edge-iad3-07 SHA256:7Lp…3vT', outcome: 'ok', detail: 'approved', sourceIp: '10.7.1.1' },
    { at: '2026-08-30T08:12:00Z', actorLabel: 'unknown', action: 'login_failed', targetKind: 'user', targetLabel: 'admin', outcome: 'denied', detail: 'no such user', sourceIp: '203.0.113.7' },
    { at: '2026-08-29T22:04:00Z', actorLabel: 'rlee', action: 'api_key.revoke', targetKind: 'api_key', targetLabel: 'old-laptop', outcome: 'denied', detail: 'viewer may not revoke keys', sourceIp: '10.7.1.2' },
  ],
})

export const licenseFixture = pb(LicenseResponseSchema, {
  loaded: true,
  customer: 'Example Inc',
  edition: 'enterprise',
  nodeCeiling: 500,
  expiry: '2027-01-31T00:00:00Z',
  status: 'valid',
  message: '428 of 500 nodes in use',
  nodeCount: 428,
  state: {
    customer: 'Example Inc',
    edition: 'enterprise',
    nodeCeiling: 500,
    expiresAt: '2027-01-31T00:00:00Z',
    signatureValid: true,
    lastEvaluatedAt: '2026-08-30T09:00:00Z',
    installedByUsername: 'awong',
  },
})

export const overCeilingLicenseFixture = pb(LicenseResponseSchema, {
  loaded: true,
  customer: 'Example Inc',
  edition: 'enterprise',
  nodeCeiling: 400,
  expiry: '2026-09-15T00:00:00Z',
  status: 'over_ceiling',
  message: '428 nodes in use, ceiling is 400 — collection continues, the index stays readable',
  nodeCount: 428,
  state: { customer: 'Example Inc', edition: 'enterprise', nodeCeiling: 400, expiresAt: '2026-09-15T00:00:00Z', signatureValid: true, lastEvaluatedAt: '2026-08-30T09:00:00Z', installedByUsername: 'awong' },
})

export const diagnosticsFixture = pb(DiagnosticsResponseSchema, {
  version: '0.9.3',
  goVersion: 'go1.25.1',
  uptimeSeconds: 421_200n,
  dbPath: '/var/lib/nagipath/nagipath.db',
  dbSizeBytes: 4_294_967_296n,
  masterKeyPath: '/var/lib/nagipath/master.key',
  masterKeyPresent: true,
  masterKeyMode: 'file (0600)',
  listenAddr: '0.0.0.0:8080',
  tlsEnabled: true,
  demoMode: true,
  licenseStatus: 'valid',
  licenseMessage: '428 of 500 nodes in use',
  migrationsApplied: 34,
  migrationsExpected: 34,
})

export const collectionsFixture = pb(CollectionsResponseSchema, {
  total: 41230,
  succeeded: 1024,
  degraded: 31,
  failed: 6,
  running: 2,
  medianMs: 1840n,
  p95Ms: 6120n,
  from: 1,
  to: 5,
  hasMore: true,
  nextCursor: '5495',
  rows: [
    { id: 5501n, nodeId: 104n, nodeName: 'lb-legacy-04', trigger: 'schedule', startedAt: '2026-08-30T09:40:00Z', status: 'failed', error: 'ssh: handshake failed — host key changed', instancesSeen: 0, durationMs: 4120n, outcome: 'failed' },
    { id: 5500n, nodeId: 12n, nodeName: 'app-iad3-01', trigger: 'manual', startedAt: '2026-08-30T09:38:00Z', status: 'succeeded', instancesSeen: 2, durationMs: 1720n, outcome: 'unchanged' },
    { id: 5499n, nodeId: 41n, nodeName: 'app-iad3-17', trigger: 'schedule', startedAt: '2026-08-30T09:35:00Z', status: 'degraded', error: '2 includes unreadable', instancesSeen: 2, durationMs: 2240n, outcome: 'changed' },
    { id: 5498n, nodeId: 88n, nodeName: 'app-sfo2-04', trigger: 'schedule', startedAt: '2026-08-30T09:20:00Z', status: 'succeeded', instancesSeen: 1, durationMs: 1520n, outcome: 'changed' },
    { id: 5497n, nodeId: 210n, nodeName: 'edge-sfo2-02', trigger: 'schedule', startedAt: '2026-08-30T09:15:00Z', status: 'running', instancesSeen: 0, durationMs: 0n, outcome: '' },
  ],
})
