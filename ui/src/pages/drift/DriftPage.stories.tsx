import type { Meta, StoryObj } from '@storybook/react-vite'
import { DriftPage } from './DriftPage'
import { pageMeta } from '../../stories/support/story'
import { driftFixture, emptyDriftFixture } from '../../stories/fixtures/drift'
import { clustersFixture } from '../../stories/fixtures/clusters'

export default {
  title: 'Pages/Drift/Overview',
  ...pageMeta({
    component: DriftPage,
    screen: '6b',
    route: '/drift',
    // edge-sfo2's group has no baseline, exercising the "Set baseline" picker
    // — clustersFixture answers its member list (see that fixture's note on
    // why the mock ignores which cluster actually asked).
    api: { '/drift': driftFixture, '/clusters': clustersFixture },
  }),
} satisfies Meta

export const Default: StoryObj = {}

// Everything matches its golden peer — the good state still has to read well.
export const NoDrift: StoryObj = { parameters: { api: { '/drift': emptyDriftFixture } } }
