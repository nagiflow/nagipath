import type { Meta, StoryObj } from '@storybook/react-vite'
import { CollectionsPage } from './CollectionsPage'
import { pageMeta } from '../../stories/support/story'
import { collectionsFixture } from '../../stories/fixtures/settings'

// Lives at /collections, not under /settings — it is a run log, not a setting,
// even though design/ files it as screen 8d.
export default {
  title: 'Pages/Settings/Collection jobs',
  ...pageMeta({
    component: CollectionsPage,
    screen: '8d',
    route: '/collections',
    api: { '/collections': collectionsFixture },
  }),
} satisfies Meta

export const Default: StoryObj = {}
