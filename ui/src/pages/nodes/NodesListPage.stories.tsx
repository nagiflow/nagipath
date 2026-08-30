import type { Meta, StoryObj } from '@storybook/react-vite'
import { NodesListPage } from './NodesListPage'
import { pageMeta } from '../../stories/support/story'
import { emptyNodesListFixture, nodesListFixture } from '../../stories/fixtures/nodes'
import { clustersFixture } from '../../stories/fixtures/clusters'

export default {
  title: 'Pages/Nodes/List',
  ...pageMeta({
    component: NodesListPage,
    screen: '2c',
    route: '/nodes',
    // One screen, two endpoints: the clusters band and the node table.
    api: { '/nodes': nodesListFixture, '/clusters': clustersFixture },
  }),
} satisfies Meta

export const Default: StoryObj = {}

export const NoMatches: StoryObj = {
  parameters: { router: { route: '/nodes?q=nothing-matches-this' }, api: { '/nodes': emptyNodesListFixture } },
}
