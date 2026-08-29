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

export interface ClusterListItem {
  id: number
  name: string
  members: number
  vendor: string
  golden_peer_name?: string
  drift_count: number
  certs_expiring_30d: number
  last_collected?: string
  instances_collected: number
}

export interface ClusterMemberItem {
  id: number
  display_name: string
  divergence?: number
  is_golden: boolean
}

export interface ClusterDetail {
  id: number
  name: string
  members: number
  vendor?: string
  golden_peer_name?: string
  last_collected?: string
  member_list: ClusterMemberItem[]
}

export interface ClustersResponse {
  clusters: ClusterListItem[]
  total: number
  query: string
  drift_filter: string
  sort: string
  selected?: ClusterDetail
}

export interface SiteListRow {
  Name: string
  AliasCount: number
  Aliases: string[]
  Nodes: number
  ListenerSummary: string
  CertSubject: string
  Routes: number
  Variants: number
  State: string
  StateReason: string
}

export interface SiteStats {
  Hostnames: number
  Nodes: number
  VariantHosts: number
  TLSTerminated: number
  Plaintext: number
  ExpiringCerts: number
  ExpiringBindings: number
}

export interface SiteVariant {
  Key: string
  RawText: string
  Nodes: number
  NodeNames: string[]
  Routes: unknown[]
}

export interface SitesListResponse {
  Rows: SiteListRow[]
  Query: string
  Total: number
  Stats: SiteStats
  Sel: string
  Variants: SiteVariant[]
}

export interface SiteTabItem {
  Label: string
  Href: string
  Count: number
  On: boolean
}

export interface SiteVariantOpt {
  Key: string
  Label: string
  On: boolean
}

export interface ClusterCount {
  Name: string
  Nodes: number
}

export interface UpstreamSummary {
  Name: string
  Members: number
  Variant: string
}

export interface SiteDetailRoute {
  Ordinal: number
  Pattern: string
  MatchType: string
  Action: string
  Target: string
  AlsoDoes: string
  Variant: string
}

export interface SiteDetailStats {
  Nodes: number
  Routes: number
  Variants: number
  Upstreams: number
  CertDays: number
  CertSubject: string
}

export interface SiteOverview {
  Name: string
  Aliases: string[]
  Variants: number
  VariantKey: string
  Nodes: number
  Clusters: ClusterCount[]
  ListenerSummary: string
  ListenerFlags: string
  CertSubject: string
  CertIssuer: string
  CertExpiry: string
  CertExpiryDays: number
  CertBindings: number
  CertUncovered: string[]
  Upstreams: UpstreamSummary[]
  Routes: SiteDetailRoute[]
  Stats: SiteDetailStats
}

export interface SiteNodeRow {
  NodeID: number
  NodeName: string
  Cluster: string
  Variant: string
  Listener: string
  Certificate: string
  LastColl: string
  State: string
  StateReason: string
}

export interface SiteUpstreamMember {
  Upstream: string
  Host: string
  Port: number
  Scheme: string
  Weight: number
  Flags: string
  NodeName: string
}

export interface SiteCertBinding {
  Subject: string
  SANs: string[]
  Issuer: string
  NotAfter: string
  ExpiryDays: number
  Bindings: number
  Uncovered: string[]
}

export interface SiteDetailResponse {
  Name: string
  Tab: string
  Variant: string
  Overview?: SiteOverview
  Nodes?: SiteNodeRow[]
  Upstreams?: SiteUpstreamMember[]
  Certs?: SiteCertBinding[]
  Tabs: SiteTabItem[]
  VariantOpts: SiteVariantOpt[]
}

export interface SessionResponse {
  user: SessionUser
  csrf_token: string
  must_change_password: boolean
  nav_counts: NavCounts
  license_notice: string
  demo_mode: boolean
}
