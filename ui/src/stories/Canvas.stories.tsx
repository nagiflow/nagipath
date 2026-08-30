import { useState, type ComponentType } from 'react'
import type { Meta, StoryObj } from '@storybook/react-vite'
import type { JsonValue } from '@bufbuild/protobuf'
import { fromJson } from '@bufbuild/protobuf'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { SessionResponseSchema } from '../api/pb/nagipath/api/v1/session_pb'
import { AppShell } from '../components/layout/AppShell'
import type { ApiFixtures } from './support/mockApi'
import { screens } from './support/story'

// One canvas holding every ported page at once, in screen order — a wall of
// every screen, each tile the real page with real API shapes. The tiles are
// built from the page stories themselves (their component, route and
// fixtures), so a new page story shows up here without touching this file.
const modules = import.meta.glob<Record<string, unknown>>('../pages/**/*.stories.tsx', { eager: true })

// Every screen was designed at this size, so the tiles render at it too — a
// page that only lines up at some other viewport is not the screen it claims to port.
const WIDTH = 1400
const HEIGHT = 880

interface StoryParams {
  screen?: string
  shell?: boolean
  router?: { route?: string; path?: string }
  api?: ApiFixtures
}

interface Tile {
  key: string
  screen?: string
  title: string
  route: string
  path?: string
  shell: boolean
  component: ComponentType
  api: ApiFixtures
}

const tiles: Tile[] = []
for (const mod of Object.values(modules)) {
  const meta = mod.default as Meta & { parameters?: StoryParams }
  const base = meta.parameters ?? {}
  const add = (name: string, own: StoryParams) =>
    tiles.push({
      key: `${meta.title ?? ''}/${name}`,
      screen: own.screen ?? base.screen,
      title: meta.title ?? '',
      route: own.router?.route ?? base.router?.route ?? '/',
      path: own.router?.path ?? base.router?.path,
      shell: (own.shell ?? base.shell) !== false,
      component: meta.component as ComponentType,
      api: { ...base.api, ...own.api },
    })

  add('default', {})
  // A named story that claims its own screen earns its own tile: the node
  // detail page is five screens (3c, 5a, 2o, 2p, 5b) behind one component.
  for (const [name, story] of Object.entries(mod)) {
    if (name === 'default') continue
    const own = (story as StoryObj).parameters as StoryParams | undefined
    if (own?.screen) add(name, own)
  }
}

const order = new Map(screens.map((screen, i) => [screen.id, i]))
const rank = (t: Tile) => order.get(t.screen ?? '') ?? screens.length
tiles.sort((a, b) => rank(a) - rank(b) || a.key.localeCompare(b.key))

// ponytail: one fixture map for the whole canvas, so an endpoint two pages read
// answers both with the same body — which is what you want anyway on a wall
// meant to look like one system. /session is the exception: the auth screens
// need an anonymous one while every other tile needs its shell, so it is primed
// per tile below instead of merged in here.
const canvasFixtures: ApiFixtures = {}
for (const tile of tiles) {
  for (const [path, body] of Object.entries(tile.api)) {
    if (path !== '/session') canvasFixtures[path] = body
  }
}

const titleOf = (id?: string) => screens.find((s) => s.id === id)?.title ?? ''

function Tile({ tile, scale }: { tile: Tile; scale: number }) {
  const [client] = useState(() => {
    // staleTime Infinity: 36 pages mount at once, and nothing here should
    // refetch behind your back while you are comparing two tiles.
    const c = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } })
    const session = tile.api['/session']
    if (session) c.setQueryData(['session'], fromJson(SessionResponseSchema, session as JsonValue))
    return c
  })

  const Page = tile.component
  const page = <Page />
  const body = tile.shell ? <AppShell>{page}</AppShell> : page

  return (
    <figure>
      <figcaption>
        <b>{tile.screen ?? '—'}</b>
        {titleOf(tile.screen) || tile.title} <code>{tile.route}</code>
      </figcaption>
      <div className="sb-frame" style={{ width: WIDTH * scale, height: HEIGHT * scale }}>
        <div style={{ width: WIDTH, height: HEIGHT, transform: `scale(${scale})` }}>
          <QueryClientProvider client={client}>
            <MemoryRouter initialEntries={[tile.route]}>
              {tile.path ? <Routes><Route path={tile.path} element={body} /></Routes> : body}
            </MemoryRouter>
          </QueryClientProvider>
        </div>
      </div>
    </figure>
  )
}

function Canvas({ scale }: { scale: number }) {
  return (
    <div className="sb-canvas">
      {tiles.map((tile) => <Tile key={tile.key} tile={tile} scale={scale} />)}
    </div>
  )
}

export default {
  title: 'Design/Canvas',
  parameters: {
    layout: 'fullscreen',
    // The canvas builds its own routers, shells and query clients, one per tile.
    shell: false,
    router: false,
    api: canvasFixtures,
    docs: {
      description: {
        component:
          'Every page at once, in the design file’s screen order. Zoom with the `scale` control; open a tile’s own story under **Pages** to click around in it.',
      },
    },
  },
  argTypes: { scale: { control: { type: 'range', min: 0.2, max: 1, step: 0.05 } } },
  args: { scale: 0.35 },
} satisfies Meta<{ scale: number }>

export const AllScreens: StoryObj<{ scale: number }> = {
  name: 'All screens',
  render: ({ scale }) => <Canvas scale={scale} />,
}
