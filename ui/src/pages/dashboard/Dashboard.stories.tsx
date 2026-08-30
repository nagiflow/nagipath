import type { Meta, StoryObj } from '@storybook/react-vite'
import { Dashboard } from './Dashboard'
import { pageMeta } from '../../stories/support/story'
import { dashboardFixture, emptyDashboardFixture } from '../../stories/fixtures/dashboard'

export default {
  title: 'Pages/Dashboard',
  ...pageMeta({
    component: Dashboard,
    screen: '2j',
    route: '/',
    api: { '/dashboard': dashboardFixture },
  }),
} satisfies Meta

export const Default: StoryObj = {}

// Fresh install: no nodes, no runs, so every band collapses to its prompt.
export const Empty: StoryObj = { parameters: { api: { '/dashboard': emptyDashboardFixture } } }
