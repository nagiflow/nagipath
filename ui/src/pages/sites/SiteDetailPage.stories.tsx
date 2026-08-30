import type { Meta, StoryObj } from '@storybook/react-vite'
import { SiteDetailPage } from './SiteDetailPage'
import { pageMeta } from '../../stories/support/story'
import { siteDetailFixture } from '../../stories/fixtures/sites'

export default {
  title: 'Pages/Sites/Detail',
  ...pageMeta({
    component: SiteDetailPage,
    screen: '7a',
    route: '/sites/checkout.example.com',
    path: '/sites/:name',
    api: { '/sites/checkout.example.com': siteDetailFixture },
  }),
} satisfies Meta

export const Overview: StoryObj = {}

// The variant selector is a URL param, so a deep link opens on one variant.
export const OneVariant: StoryObj = {
  parameters: { router: { route: '/sites/checkout.example.com?variant=a1b2c3', path: '/sites/:name' } },
}
