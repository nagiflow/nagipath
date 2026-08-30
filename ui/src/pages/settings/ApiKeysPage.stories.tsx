import type { Meta, StoryObj } from '@storybook/react-vite'
import { ApiKeysPage } from './ApiKeysPage'
import { pageMeta } from '../../stories/support/story'
import { apiKeysFixture } from '../../stories/fixtures/settings'

export default {
  title: 'Pages/Settings/API keys',
  ...pageMeta({
    component: ApiKeysPage,
    screen: '8g',
    route: '/settings/api-keys',
    api: { '/settings/api-keys': apiKeysFixture },
  }),
} satisfies Meta

export const Default: StoryObj = {}
