import type { Meta, StoryObj } from '@storybook/react-vite'
import { AppShell } from './AppShell'
import { PageHeader, Panel } from '../ui'
import { sessionFixture, viewerSessionFixture } from '../../stories/fixtures/session'

// The chrome every page renders inside. `shell: false` here because the story
// IS the shell — the preview's withShell decorator would otherwise nest two.
const meta: Meta<typeof AppShell> = {
  title: 'Components/App shell',
  component: AppShell,
  parameters: {
    layout: 'fullscreen',
    shell: false,
    api: { '/session': sessionFixture },
    docs: {
      description: {
        component:
          'Sidebar, header, breadcrumb and nav counts, all driven by `GET /api/session` — nav structure lives in `navConfig.ts`, the breadcrumb is derived from the path. Admin-only items and the counts change with the session, so both roles get a story.',
      },
    },
  },
  render: (_args, { parameters }) => (
    <AppShell>
      <PageHeader title={String(parameters.router?.route ?? '/')} meta="story body — the page renders here" />
      <div className="bd">
        <Panel><span className="m mu">Page content occupies `.main`, which scrolls independently of the sidebar.</span></Panel>
      </div>
    </AppShell>
  ),
}
export default meta

type Story = StoryObj<typeof meta>

export const Admin: Story = { parameters: { router: { route: '/' } } }

// A viewer loses the admin-only nav items and the DEMO badge stays put.
export const Viewer: Story = {
  parameters: { router: { route: '/nodes' }, api: { '/session': viewerSessionFixture } },
}

// Deep route: breadcrumb shows section / page, and the matching nav item is `.on`.
export const NestedRoute: Story = { parameters: { router: { route: '/settings/retention' } } }
