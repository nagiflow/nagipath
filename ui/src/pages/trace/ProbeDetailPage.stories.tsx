import type { Meta, StoryObj } from '@storybook/react-vite'
import { ProbeDetailPage } from './ProbeDetailPage'
import { pageMeta } from '../../stories/support/story'
import { probeDetailFixture } from '../../stories/fixtures/trace'

export default {
  title: 'Pages/Trace/Probe detail',
  ...pageMeta({
    component: ProbeDetailPage,
    screen: '2h',
    route: '/trace/probe?probe=5001',
    api: { '/trace/probe/5001': probeDetailFixture },
  }),
} satisfies Meta

export const Default: StoryObj = {}
