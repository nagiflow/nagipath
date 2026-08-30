import type { Meta, StoryObj } from '@storybook/react-vite'
import { CredentialsPage } from './CredentialsPage'
import { pageMeta } from '../../stories/support/story'
import { credentialsFixture } from '../../stories/fixtures/settings'

export default {
  title: 'Pages/Settings/Credentials',
  ...pageMeta({
    component: CredentialsPage,
    screen: '8a',
    route: '/settings/credentials',
    api: { '/settings/credentials': credentialsFixture },
  }),
} satisfies Meta

export const Default: StoryObj = {}
