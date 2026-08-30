import type { Meta, StoryObj } from '@storybook/react-vite'
import { UsersPage } from './UsersPage'
import { pageMeta } from '../../stories/support/story'
import { usersFixture } from '../../stories/fixtures/settings'

// design/ draws Users as part of screen 2v's settings list rather than giving
// it its own canvas; it follows the 8a-8h sub-page shape exactly.
export default {
  title: 'Pages/Settings/Users',
  ...pageMeta({
    component: UsersPage,
    screen: '2v',
    route: '/settings/users',
    api: { '/settings/users': usersFixture },
  }),
} satisfies Meta

export const Default: StoryObj = {}
