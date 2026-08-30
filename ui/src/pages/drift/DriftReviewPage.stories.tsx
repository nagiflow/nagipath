import type { Meta, StoryObj } from '@storybook/react-vite'
import { DriftReviewPage } from './DriftReviewPage'
import { pageMeta } from '../../stories/support/story'
import { driftReviewFixture } from '../../stories/fixtures/drift'

export default {
  title: 'Pages/Drift/Review',
  ...pageMeta({
    component: DriftReviewPage,
    screen: '6a',
    route: '/drift/review/900',
    path: '/drift/review/:instanceID',
    api: { '/drift/review/900': driftReviewFixture },
  }),
} satisfies Meta

export const Default: StoryObj = {}
