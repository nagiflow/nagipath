import { useState, type ReactNode } from 'react'
import type { Decorator, Preview } from '@storybook/react-vite'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import '../src/theme/fonts.css'
import '../src/index.css'
import '../src/stories/support/canvas.css'
import { AppShell } from '../src/components/layout/AppShell'
import { sessionFixture } from '../src/stories/fixtures/session'
import { diagnosticsFixture } from '../src/stories/fixtures/settings'
import { installApiMock, setApiFixtures } from '../src/stories/support/mockApi'

installApiMock()

// The two endpoints the chrome itself reads, on every story: /session (AppShell
// won't draw without it) and /settings/system (SettingsLayout's "This install"
// panel). A story can override either — see Pages/Auth and Pages/Settings/System.
const chrome = { '/session': sessionFixture, '/settings/system': diagnosticsFixture }

// Fixtures are registered before the story's hooks run (this is the outermost
// decorator), so a page's first render already has its data in flight.
const withApi: Decorator = (Story, ctx) => {
  setApiFixtures({ ...chrome, ...(ctx.parameters.api ?? {}) })
  return <Story />
}

// One client per story: no cache bleeding between stories, and `retry: false`
// so an intentionally-missing fixture shows its error state immediately.
function WithQueryClient({ children }: { children: ReactNode }) {
  const [client] = useState(() => new QueryClient({ defaultOptions: { queries: { retry: false } } }))
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>
}

const withQueryClient: Decorator = (Story) => (
  <WithQueryClient>
    <Story />
  </WithQueryClient>
)

// parameters.router = { route: '/nodes/42?tab=routes', path: '/nodes/:id' }.
// `path` is only needed by pages that read useParams(); `router: false` is for
// a story that builds its own routers (Design › Canvas — react-router refuses
// to render a Router inside a Router).
const withRouter: Decorator = (Story, ctx) => {
  if (ctx.parameters.router === false) return <Story />
  const { route = '/', path } = ctx.parameters.router ?? {}
  const story = <Story />
  return (
    <MemoryRouter initialEntries={[route]}>
      {path ? <Routes><Route path={path} element={story} /></Routes> : story}
    </MemoryRouter>
  )
}

// parameters.shell: page stories render inside the app's real chrome (header,
// sidebar, breadcrumb) so they line up 1:1 with the wireframe screen they port.
const withShell: Decorator = (Story, ctx) =>
  ctx.parameters.shell ? <AppShell><Story /></AppShell> : <Story />

const preview: Preview = {
  // Outermost first: api > queryClient > router > shell > story.
  decorators: [withShell, withRouter, withQueryClient, withApi],
  parameters: {
    controls: { expanded: true },
    options: {
      // Docs before components before pages — the order a designer reads them in.
      storySort: {
        order: ['Design', ['Introduction', 'Design tokens', 'Screens', 'Canvas', 'Contributing'], 'Components', 'Pages'],
      },
    },
  },
}

export default preview
