import type { Meta, StoryObj } from '@storybook/react-vite'
import { ProbeHistoryPage } from './ProbeHistoryPage'
import { pageMeta } from '../../stories/support/story'
import { probeHistoryFixture } from '../../stories/fixtures/trace'

export default {
  title: 'Pages/Trace/Probe history',
  ...pageMeta({
    component: ProbeHistoryPage,
    screen: '2w',
    route: '/trace/history',
    api: { '/trace/history': probeHistoryFixture },
  }),
} satisfies Meta

export const Default: StoryObj = {}

// The row detail is this screen's whole point — an audit record and its state
// changes expanded under the row, never a right-hand preview pane — so it
// gets its own story rather than only being reachable by clicking a row live.
export const RowExpanded: StoryObj = { parameters: { router: { route: '/trace/history?probe=5001' } } }
