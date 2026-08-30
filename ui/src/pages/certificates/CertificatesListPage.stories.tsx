import type { Meta, StoryObj } from '@storybook/react-vite'
import { CertificatesListPage } from './CertificatesListPage'
import { pageMeta } from '../../stories/support/story'
import { certificateDetailFixture, certificatesListFixture } from '../../stories/fixtures/certificates'

export default {
  title: 'Pages/Certificates/List',
  ...pageMeta({
    component: CertificatesListPage,
    screen: '2f',
    route: '/certificates',
    // Expanding a row fetches that certificate, same endpoint the detail page uses.
    api: { '/certificates': certificatesListFixture, '/certificates/701': certificateDetailFixture },
  }),
} satisfies Meta

export const Default: StoryObj = {}
