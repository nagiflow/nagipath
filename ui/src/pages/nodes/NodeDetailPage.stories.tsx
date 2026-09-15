import type { Meta, StoryObj } from '@storybook/react-vite'
import { NodeDetailPage } from './NodeDetailPage'
import { pageMeta } from '../../stories/support/story'
import {
  nodeCertificatesFixture, nodeDetailFixture, nodeDriftFixture, nodeFilesFixture, nodeLiveFileFixture,
  nodeRoutesFixture, nodeUpstreamsFixture,
} from '../../stories/fixtures/nodes'

// Both write flows (edit a file on the node, restart a process) start behind a
// button, so the story presses it. No user-event dependency: these are two
// plain clicks on a button found by its label.
const clickButton = (label: string) => async ({ canvasElement }: { canvasElement: HTMLElement }) => {
  const button = [...canvasElement.querySelectorAll('button')].find((b) => b.textContent === label)
  button?.click()
}

// One page, six tabs, each its own wireframe screen — the tab is a URL param
// and a path segment on the API (`GET /nodes/41/routes`), so each tab is a
// story with its own fixture.
export default {
  title: 'Pages/Nodes/Detail',
  ...pageMeta({
    component: NodeDetailPage,
    screen: '3c',
    route: '/nodes/41',
    path: '/nodes/:id',
    api: { '/nodes/41': nodeDetailFixture },
  }),
} satisfies Meta

// `screen` is the wireframe id for that tab (Design › Screens, Design › Canvas);
// the overview's 3c comes from the meta above.
const tab = (name: string, fixture: unknown, screen?: string): StoryObj => ({
  parameters: {
    screen,
    router: { route: `/nodes/41?tab=${name}`, path: '/nodes/:id' },
    api: { [`/nodes/41/${name}`]: fixture },
  },
})

export const Overview: StoryObj = {}
export const Routes = tab('routes', nodeRoutesFixture, '5a')
export const Upstreams = tab('upstreams', nodeUpstreamsFixture, '2o')
export const Certificates = tab('certificates', nodeCertificatesFixture, '2p')
export const ConfigFiles = tab('files', nodeFilesFixture, '5b')
export const Drift = tab('drift', nodeDriftFixture)

// The live file behind the capture, once an admin presses "Edit on node":
// read fresh over SSH, written back with a backup beside it.
export const ConfigFilesEditing: StoryObj = {
  parameters: {
    screen: '5b',
    router: { route: '/nodes/41?tab=files', path: '/nodes/:id' },
    api: { '/nodes/41/files': nodeFilesFixture, '/nodes/41/livefile': nodeLiveFileFixture },
  },
  play: clickButton('Edit on node'),
}

// Restarting the selected process. The command is the one stored on the
// Instance, editable before it runs — nagipath does not guess how a fleet
// restarts a process.
export const RestartProcess: StoryObj = {
  play: clickButton('Restart process'),
}
