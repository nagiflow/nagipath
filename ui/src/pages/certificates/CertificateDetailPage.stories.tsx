import type { Meta, StoryObj } from '@storybook/react-vite'
import { CertificateDetailPage } from './CertificateDetailPage'
import { pageMeta } from '../../stories/support/story'
import { certificateDetailFixture } from '../../stories/fixtures/certificates'

export default {
  title: 'Pages/Certificates/Detail',
  ...pageMeta({
    component: CertificateDetailPage,
    screen: '7b',
    route: '/certificates/701',
    path: '/certificates/:id',
    api: { '/certificates/701': certificateDetailFixture },
  }),
} satisfies Meta

export const Bindings: StoryObj = {}

export const Files: StoryObj = {
  parameters: { router: { route: '/certificates/701?tab=files', path: '/certificates/:id' } },
}
