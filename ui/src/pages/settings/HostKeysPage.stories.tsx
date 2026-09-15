import type { Meta, StoryObj } from '@storybook/react-vite'
import { HostKeysPage } from './HostKeysPage'
import { pageMeta } from '../../stories/support/story'
import { hostKeysFixture, hostKeysTofuFixture } from '../../stories/fixtures/settings'

export default {
  title: 'Pages/Settings/Host keys',
  ...pageMeta({
    component: HostKeysPage,
    screen: '8b',
    route: '/settings/hostkeys',
    api: { '/settings/hostkeys': hostKeysFixture },
  }),
} satisfies Meta

export const Default: StoryObj = {}

export const TrustOnFirstUse: StoryObj = {
  parameters: { api: { '/settings/hostkeys': hostKeysTofuFixture } },
}
