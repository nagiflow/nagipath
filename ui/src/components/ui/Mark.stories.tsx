import type { Meta, StoryObj } from '@storybook/react-vite'
import { Mark } from './Mark'

const meta: Meta = {
  title: 'Components/Mark',
  parameters: {
    docs: {
      description: {
        component: 'The product mark — header wordmark icon, sign-in card, and the source for `public/favicon.svg`.',
      },
    },
  },
}
export default meta

export const Sizes: StoryObj = {
  render: () => (
    <div className="row" style={{ alignItems: 'center', gap: 16, color: '#0077cc' }}>
      {[16, 20, 28, 40, 64].map((px) => (
        <span key={px} style={{ width: px, height: px, display: 'inline-flex' }}><Mark /></span>
      ))}
    </div>
  ),
}
