import type { Meta, StoryObj } from '@storybook/react-vite'
import { SetupPage } from './SetupPage'
import { pageMeta } from '../../stories/support/story'
import { setupRequiredSessionFixture } from '../../stories/fixtures/session'

export default {
  title: 'Pages/Auth/First-run setup',
  ...pageMeta({
    component: SetupPage,
    screen: '9b',
    route: '/setup',
    shell: false,
    api: { '/session': setupRequiredSessionFixture },
  }),
} satisfies Meta

export const Default: StoryObj = {}
