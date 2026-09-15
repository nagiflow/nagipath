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

// The edit panel, which only exists behind two clicks (expand the row, then
// "Edit credential") — and is the one form where a blank secret field means
// "keep what is sealed" rather than "erase it", so it is worth seeing.
export const Editing: StoryObj = {
  play: async ({ canvasElement }: { canvasElement: HTMLElement }) => {
    const row = [...canvasElement.querySelectorAll('tbody tr')][0]
    row?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await new Promise((r) => setTimeout(r, 0))
    const button = [...canvasElement.querySelectorAll('button')].find((b) => b.textContent === 'Edit credential')
    button?.click()
  },
}
