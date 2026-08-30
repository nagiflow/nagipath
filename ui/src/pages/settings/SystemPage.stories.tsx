import type { Meta, StoryObj } from '@storybook/react-vite'
import { SystemPage } from './SystemPage'
import { pageMeta } from '../../stories/support/story'

// /settings/system is a chrome endpoint (the sidebar's "This install" panel), so
// its fixture is already registered for every story — see .storybook/preview.tsx.
export default {
  title: 'Pages/Settings/System',
  ...pageMeta({
    component: SystemPage,
    screen: '8j',
    route: '/settings/system',
  }),
} satisfies Meta

export const Default: StoryObj = {}
