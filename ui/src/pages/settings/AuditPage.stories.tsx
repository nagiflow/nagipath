import type { Meta, StoryObj } from '@storybook/react-vite'
import { AuditPage } from './AuditPage'
import { pageMeta } from '../../stories/support/story'
import { auditFixture } from '../../stories/fixtures/settings'

export default {
  title: 'Pages/Settings/Audit log',
  ...pageMeta({
    component: AuditPage,
    screen: '8h',
    route: '/settings/audit',
    api: { '/settings/audit': auditFixture },
  }),
} satisfies Meta

export const Default: StoryObj = {}
