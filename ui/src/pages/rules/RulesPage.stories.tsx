import type { Meta, StoryObj } from '@storybook/react-vite'
import { RulesPage } from './RulesPage'
import { pageMeta } from '../../stories/support/story'
import { emptyRulesFixture, rulesFixture, unaskedRulesFixture } from '../../stories/fixtures/rules'

export default {
  title: 'Pages/Rule lookup',
  ...pageMeta({
    component: RulesPage,
    screen: '2b',
    route: '/rules?url=https://checkout.example.com/api/v2/cart',
    api: { '/rules': rulesFixture },
  }),
} satisfies Meta

export const Default: StoryObj = {}

// Nothing asked yet — the page opens on the form, not on an empty result table.
export const Unasked: StoryObj = { parameters: { router: { route: '/rules' }, api: { '/rules': unaskedRulesFixture } } }

// Asked, but no rule in the fleet matches that URL.
export const NoMatches: StoryObj = {
  parameters: { router: { route: '/rules?url=https://nothing.example.com/' }, api: { '/rules': emptyRulesFixture } },
}
