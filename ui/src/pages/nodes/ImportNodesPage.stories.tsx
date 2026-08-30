import type { Meta, StoryObj } from '@storybook/react-vite'
import { ImportNodesPage } from './ImportNodesPage'
import { pageMeta } from '../../stories/support/story'
import {
  importNodesFixture,
  importTestConnectedFixture,
  importTestFailedFixture,
  importTestHostKeyPendingFixture,
} from '../../stories/fixtures/nodes'
import { credentialsFixture } from '../../stories/fixtures/settings'

// Story-level `parameters.api` merges into the meta-level map above (see
// RetentionPage.stories.tsx's Viewer story), so each variant below only lists
// the endpoint it actually changes.

export default {
  title: 'Pages/Nodes/Import inventory',
  ...pageMeta({
    component: ImportNodesPage,
    screen: '2q',
    route: '/nodes/import',
    // Credentials populate the picker; /nodes/import answers step 1's paste,
    // and each /nodes/{id}/test answers step 2's automatic per-row test —
    // one of each outcome, matching importNodesFixture's four added nodes —
    // so pasting a host list and continuing shows the whole wizard for real.
    api: {
      '/settings/credentials': credentialsFixture,
      '/nodes/import': importNodesFixture,
      '/nodes/431/test': importTestConnectedFixture,
      '/nodes/432/test': importTestHostKeyPendingFixture,
      '/nodes/433/test': importTestFailedFixture,
      '/nodes/434/test': importTestConnectedFixture,
    },
  }),
} satisfies Meta

export const Default: StoryObj = {}

// Every line refused: step 1 stays put with the reasons, rather than
// advancing to an empty step 2.
export const AllRefused: StoryObj = {
  parameters: {
    api: {
      '/nodes/import': {
        added: [],
        refused: ['10.90.4.0/24: nagipath does not scan networks; add one host at a time'],
      },
    },
  },
}

export const NoCredentialsYet: StoryObj = {
  parameters: { api: { '/settings/credentials': { credentials: [] } } },
}
