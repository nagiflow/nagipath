import type { Meta, StoryObj } from '@storybook/react-vite'
import { LicensePage } from './LicensePage'
import { pageMeta } from '../../stories/support/story'
import { licenseFixture, overCeilingLicenseFixture } from '../../stories/fixtures/settings'

export default {
  title: 'Pages/Settings/License',
  ...pageMeta({
    component: LicensePage,
    screen: '8i',
    route: '/settings/license',
    api: { '/settings/license': licenseFixture },
  }),
} satisfies Meta

export const Default: StoryObj = {}

// Over the node ceiling: reported, never enforced by blocking collection.
export const OverCeiling: StoryObj = { parameters: { api: { '/settings/license': overCeilingLicenseFixture } } }
