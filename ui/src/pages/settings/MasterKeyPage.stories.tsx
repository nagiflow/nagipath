import type { Meta, StoryObj } from '@storybook/react-vite'
import { MasterKeyPage } from './MasterKeyPage'
import { pageMeta } from '../../stories/support/story'
import { masterKeyFixture } from '../../stories/fixtures/settings'

export default {
  title: 'Pages/Settings/Master key',
  ...pageMeta({
    component: MasterKeyPage,
    screen: '8e',
    route: '/settings/masterkey',
    api: { '/settings/masterkey': masterKeyFixture },
  }),
} satisfies Meta

export const Default: StoryObj = {}
