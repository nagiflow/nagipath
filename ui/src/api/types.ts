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

export interface SessionResponse {
  user: SessionUser
  csrf_token: string
  nav_counts: NavCounts
  license_notice: string
}
