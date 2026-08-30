import { SessionResponseSchema } from '../../api/pb/nagipath/api/v1/session_pb'
import { pb } from '../support/mockApi'

// The signed-in admin every page story runs as (registered globally in
// .storybook/preview.tsx). Counts match the fleet the other fixtures describe,
// so the sidebar badges agree with the pages they link to.
export const sessionFixture = pb(SessionResponseSchema, {
  authenticated: true,
  csrfToken: 'storybook',
  user: { id: 1n, username: 'awong', role: 'admin' },
  navCounts: { nodes: 428, sites: 1129, clusters: 5, drift: 12, certificates: 41 },
  demoMode: true,
})

export const viewerSessionFixture = pb(SessionResponseSchema, {
  authenticated: true,
  csrfToken: 'storybook',
  user: { id: 7n, username: 'rlee', role: 'viewer' },
  navCounts: { nodes: 428, sites: 1129, clusters: 5, drift: 12, certificates: 41 },
})

export const anonymousSessionFixture = pb(SessionResponseSchema, { authenticated: false })

export const setupRequiredSessionFixture = pb(SessionResponseSchema, {
  authenticated: false,
  setupRequired: true,
})

export const mustChangePasswordSessionFixture = pb(SessionResponseSchema, {
  authenticated: true,
  csrfToken: 'storybook',
  user: { id: 9n, username: 'newadmin', role: 'admin' },
  mustChangePassword: true,
})
