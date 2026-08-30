import type { Meta } from '@storybook/react-vite'
import { screens } from './story'

// Every page story's meta, keyed by the wireframe screen it declares in
// pageMeta({ screen }). Built from the story files themselves so the index in
// Design › Screens cannot drift from the stories — a screen with no page story
// shows up as "not ported" without anyone maintaining a second list.
const modules = import.meta.glob<{ default: Meta }>('../../pages/**/*.stories.tsx', { eager: true })

interface Entry { title: string; route: string }

const byScreen = new Map<string, Entry[]>()
for (const mod of Object.values(modules)) {
  const params = mod.default.parameters as { screen?: string; router?: { route?: string } } | undefined
  if (!params?.screen) continue
  const list = byScreen.get(params.screen) ?? []
  list.push({ title: mod.default.title ?? '', route: params.router?.route ?? '' })
  byScreen.set(params.screen, list)
}

// ponytail: plain table, no sorting or filtering — 35 rows fit on one screen.
export function ScreenIndex() {
  return (
    <table>
      <thead>
        <tr><th>Screen</th><th>Design title</th><th>Route</th><th>Storybook page</th></tr>
      </thead>
      <tbody>
        {screens.map((screen) => {
          const pages = byScreen.get(screen.id) ?? []
          return (
            <tr key={screen.id}>
              <td><code>{screen.id}</code></td>
              <td>{screen.title}</td>
              <td>{pages.map((p) => <code key={p.title} style={{ marginRight: 6 }}>{p.route || '—'}</code>)}</td>
              <td>{pages.length ? pages.map((p) => p.title).join(', ') : <em>not ported</em>}</td>
            </tr>
          )
        })}
      </tbody>
    </table>
  )
}
