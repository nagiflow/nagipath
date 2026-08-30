import type { Meta, StoryObj } from '@storybook/react-vite'
import { SnapshotsListPage } from './SnapshotsListPage'
import { pageMeta } from '../../stories/support/story'
import { snapshotsListFixture } from '../../stories/fixtures/snapshots'

export default {
  title: 'Pages/Snapshots/List',
  ...pageMeta({
    component: SnapshotsListPage,
    screen: '2r',
    route: '/snapshots',
    api: { '/snapshots': snapshotsListFixture },
  }),
} satisfies Meta

export const Default: StoryObj = {}
