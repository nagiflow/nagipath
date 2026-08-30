import type { Meta, StoryObj } from '@storybook/react-vite'
import { NotFoundPage } from './NotFoundPage'

// The one page with no wireframe screen — design/ has no canvas for a bad URL,
// so it reuses EmptyPrompt inside the normal chrome and nothing more.
const meta: Meta<typeof NotFoundPage> = {
  title: 'Pages/Not found',
  component: NotFoundPage,
  parameters: { layout: 'fullscreen', shell: true, router: { route: '/nope' } },
}
export default meta

export const Default: StoryObj = {}
