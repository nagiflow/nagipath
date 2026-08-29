// Mirrors internal/api/session.go's JSON shape.
export interface SessionUser {
  id: number
  username: string
  role: 'admin' | 'viewer'
}

export interface NavCounts {
  nodes: number
  sites: number
  clusters: number
  drift: number
  certificates: number
}

export interface DashboardAttention {
  kind: string
  tone: 'ok' | 'deg' | 'err' | 'inf'
  text: string
  note: string
  link: string
  cluster?: string
  since?: string
}

export interface DashboardActivityBucket {
  label: string
  total: number
  failed: number
  degraded: number
}

export interface DashboardRiskCluster {
  id: number
  name: string
  nodes: number
  fresh: number
  fresh_percent: number
  drift: number
  certs_label: string
  state: string
  risk: number
}

export interface DashboardResponse {
  nodes: number
  instances: number
  vendor_count: number
  degraded: number
  pending_host_keys: number
  ok_instances: number
  unparsed_instances: number
  pending_instances: number
  certs_expiring_30d: number
  drifted_instances: number
  drifted_clusters: number
  fresh_instances: number
  aging_instances: number
  stale_instances: number
  total_rules: number
  cert_bindings_expiring: number
  unreachable_nodes: number
  unreachable_since?: string

  attention: DashboardAttention[]
  activity: DashboardActivityBucket[]
  activity_max: number
  risk_clusters: DashboardRiskCluster[]
  recent_traces: Record<string, unknown>[]
  recent_collections: Record<string, unknown>[]
  expiring_certificates: Record<string, unknown>[]
  clusters: { id: number; name: string }[]

  selected_cluster: number
  attention_severity: string
  attention_cluster: number

  oldest_snapshot?: string
  oldest_snapshot_node?: string
  avg_collection_duration_seconds: number
}

export interface SessionResponse {
  user: SessionUser
  csrf_token: string
  must_change_password: boolean
  nav_counts: NavCounts
  license_notice: string
  demo_mode: boolean
}
