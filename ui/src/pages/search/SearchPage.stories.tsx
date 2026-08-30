import type { Meta, StoryObj } from '@storybook/react-vite'
import { SearchPage } from './SearchPage'
import { pageMeta } from '../../stories/support/story'
import { regexErrorSearchFixture, searchFixture, unaskedSearchFixture } from '../../stories/fixtures/search'

export default {
  title: 'Pages/Config search',
  ...pageMeta({
    component: SearchPage,
    screen: '2g',
    route: '/search?q=proxy_pass',
    api: { '/search': searchFixture },
  }),
} satisfies Meta

export const Default: StoryObj = {}

export const Unasked: StoryObj = { parameters: { router: { route: '/search' }, api: { '/search': unaskedSearchFixture } } }

// A bad regex is the user's typo, not a failure — it reports inline and keeps
// the query in the box.
export const InvalidRegex: StoryObj = {
  parameters: { router: { route: '/search?q=proxy_pass(&match=regex' }, api: { '/search': regexErrorSearchFixture } },
}
