import type { Meta, StoryObj } from '@storybook/react-vite'
import { RetentionPage } from './RetentionPage'
import { pageMeta } from '../../stories/support/story'
import { retentionFixture } from '../../stories/fixtures/settings'
import { viewerSessionFixture } from '../../stories/fixtures/session'

export default {
  title: 'Pages/Settings/Retention',
  ...pageMeta({
    component: RetentionPage,
    screen: '8f',
    route: '/settings/retention',
    api: { '/settings/retention': retentionFixture },
  }),
} satisfies Meta

export const Default: StoryObj = {}

// A viewer sees the same numbers with every control read-only.
export const Viewer: StoryObj = { parameters: { api: { '/session': viewerSessionFixture } } }
