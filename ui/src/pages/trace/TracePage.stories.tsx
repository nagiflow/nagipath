import type { Meta, StoryObj } from '@storybook/react-vite'
import { TracePage } from './TracePage'
import { pageMeta } from '../../stories/support/story'
import { emptyTraceFixture, traceFixture, unaskedTraceFixture } from '../../stories/fixtures/trace'

export default {
  title: 'Pages/Trace/Trace',
  ...pageMeta({
    component: TracePage,
    screen: '2a',
    route: '/trace?url=https://checkout.example.com/api/v2/cart',
    api: { '/trace': traceFixture },
  }),
} satisfies Meta

export const Default: StoryObj = {}

export const Unasked: StoryObj = { parameters: { router: { route: '/trace' }, api: { '/trace': unaskedTraceFixture } } }

// Hostname resolves to nothing collected — the page says which step is missing
// rather than drawing an empty hop list.
export const NoPath: StoryObj = {
  parameters: { router: { route: '/trace?url=https://unknown.example.com/' }, api: { '/trace': emptyTraceFixture } },
}
