import type { Meta, StoryObj } from '@storybook/react-vite'
import { LoginPage } from './LoginPage'
import { pageMeta } from '../../stories/support/story'
import { anonymousSessionFixture } from '../../stories/fixtures/session'

// No chrome: there is no session to draw a sidebar from yet.
export default {
  title: 'Pages/Auth/Sign in',
  ...pageMeta({
    component: LoginPage,
    screen: '9a',
    route: '/login',
    shell: false,
    api: { '/session': anonymousSessionFixture },
  }),
} satisfies Meta

export const Default: StoryObj = {}
