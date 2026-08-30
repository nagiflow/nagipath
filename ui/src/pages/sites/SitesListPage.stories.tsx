import type { Meta, StoryObj } from '@storybook/react-vite'
import { SitesListPage } from './SitesListPage'
import { pageMeta } from '../../stories/support/story'
import { emptySitesListFixture, sitesListFixture, sitesListSelectedFixture } from '../../stories/fixtures/sites'

export default {
  title: 'Pages/Sites/List',
  ...pageMeta({
    component: SitesListPage,
    screen: '3a',
    route: '/sites',
    api: { '/sites': sitesListFixture },
  }),
} satisfies Meta

export const Default: StoryObj = {}

// A row expanded: its variants and the clusters serving them open underneath.
export const RowExpanded: StoryObj = {
  parameters: { router: { route: '/sites?sel=checkout.example.com' }, api: { '/sites': sitesListSelectedFixture } },
}

export const NoMatches: StoryObj = {
  parameters: { router: { route: '/sites?q=nothing-matches-this' }, api: { '/sites': emptySitesListFixture } },
}
