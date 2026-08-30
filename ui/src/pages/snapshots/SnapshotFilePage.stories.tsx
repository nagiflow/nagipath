import type { Meta, StoryObj } from '@storybook/react-vite'
import { SnapshotFilePage } from './SnapshotFilePage'
import { pageMeta } from '../../stories/support/story'
import { snapshotFileFixture } from '../../stories/fixtures/snapshots'

// Reached from any provenance link (`?b=<byte offset>`), which is why the
// anchored state is the default story: arriving here without an anchor is the
// exception, not the rule.
export default {
  title: 'Pages/Snapshots/File',
  ...pageMeta({
    component: SnapshotFilePage,
    screen: '2s',
    route: '/snapshots/8801/file/9102?b=384',
    path: '/snapshots/:id/file/:fileID',
    api: { '/snapshots/8801/file/9102': snapshotFileFixture },
  }),
} satisfies Meta

export const Anchored: StoryObj = {}
