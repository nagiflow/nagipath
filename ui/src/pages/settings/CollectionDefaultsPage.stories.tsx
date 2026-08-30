import type { Meta, StoryObj } from '@storybook/react-vite'
import { CollectionDefaultsPage } from './CollectionDefaultsPage'
import { pageMeta } from '../../stories/support/story'
import { collectionDefaultsFixture } from '../../stories/fixtures/settings'

export default {
  title: 'Pages/Settings/Collection defaults',
  ...pageMeta({
    component: CollectionDefaultsPage,
    screen: '8c',
    route: '/settings/collection-defaults',
    api: { '/settings/collection-defaults': collectionDefaultsFixture },
  }),
} satisfies Meta

export const Default: StoryObj = {}
