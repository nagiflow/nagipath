import type { Meta, StoryObj } from '@storybook/react-vite'
import { PasswordPage } from './PasswordPage'
import { pageMeta } from '../../stories/support/story'
import { mustChangePasswordSessionFixture, sessionFixture } from '../../stories/fixtures/session'

// Inside the chrome, unlike Sign in and Setup — the user is already signed in.
export default {
  title: 'Pages/Auth/Change password',
  ...pageMeta({
    component: PasswordPage,
    screen: '8k',
    route: '/password',
    api: { '/session': sessionFixture },
  }),
} satisfies Meta

export const Default: StoryObj = {}

// Forced change after an admin reset: the page says why it cannot be skipped.
export const MustChange: StoryObj = {
  parameters: { api: { '/session': mustChangePasswordSessionFixture } },
}
