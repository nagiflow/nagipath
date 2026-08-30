import type { ComponentType } from 'react'
import type { Meta } from '@storybook/react-vite'
import type { ApiFixtures } from './mockApi'
import screensJson from '../screens.json'

// The 35 screens of this design — id, title and design notes, hand-kept
// here. Storybook is the source of truth for how a page looks; each page
// story's Docs tab carries the design notes for the screen it ports.
export interface WireframeScreenData {
  id: string
  title: string
  impl: string[]
}

export const screens = screensJson as WireframeScreenData[]

export function screenById(id: string): WireframeScreenData {
  const found = screens.find((s) => s.id === id)
  if (!found) throw new Error(`no screen "${id}" in src/stories/screens.json — add it there`)
  return found
}

// pageMeta is the boilerplate every page story would otherwise repeat: full
// chrome, a router entry, the endpoint fixtures the page reads, and the design
// notes for the screen it ports.
//
// Spread into the default export, with `title` written next to it as a literal —
// Storybook indexes story files statically and cannot read a title that only
// exists inside a function call:
//
//     export default { title: 'Pages/Dashboard', ...pageMeta({ … }) }
export function pageMeta(cfg: {
  /** Pages take no props — they read the router and the API. */
  component: ComponentType<Record<string, never>>
  /** Wireframe screen id, e.g. '3a' — see the Design › Screens index. */
  screen: string
  /** Location the story starts at, e.g. '/nodes/42?tab=routes'. */
  route?: string
  /** Route pattern, only needed by pages that read useParams(). */
  path?: string
  api?: ApiFixtures
  /** false for the two auth pages, which draw no app chrome. */
  shell?: boolean
}): Omit<Meta, 'title'> {
  const screen = screenById(cfg.screen)
  return {
    component: cfg.component,
    parameters: {
      layout: 'fullscreen',
      // Recorded so Design › Screens can build the screen → page index from the
      // page stories themselves rather than from a hand-kept second list.
      screen: cfg.screen,
      shell: cfg.shell !== false,
      router: { route: cfg.route ?? '/', path: cfg.path },
      api: cfg.api ?? {},
      docs: {
        description: {
          component: [
            `Ports screen **${screen.id} ${screen.title}**, whose design notes are:`,
            '',
            ...screen.impl.map((note) => `- ${note}`),
          ].join('\n'),
        },
      },
    },
  }
}
