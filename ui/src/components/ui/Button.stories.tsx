import type { Meta, StoryObj } from '@storybook/react-vite'
import { Button } from './Button'

const meta: Meta<typeof Button> = {
  title: 'Components/Button',
  component: Button,
  tags: ['autodocs'],
  args: { children: 'Collect now' },
  parameters: {
    docs: {
      description: {
        component: 'design/\'s `.btn`. Four variants and one size — anything else is not in the wireframe, so it should not be in the app.',
      },
    },
  },
}
export default meta

type Story = StoryObj<typeof meta>

export const Default: Story = {}
export const Primary: Story = { args: { primary: true, children: 'Save changes' } }
export const Subtle: Story = { args: { subtle: true, children: 'Cancel' } }
export const Danger: Story = { args: { danger: true, children: 'Revoke key' } }
export const Small: Story = { args: { small: true, subtle: true, children: 'Open node' } }
export const Disabled: Story = { args: { disabled: true, primary: true } }
export const Loading: Story = { args: { loading: true, primary: true } }

// A .btn that is really a link keeps the same paint — design/ draws no visual
// difference between "does something here" and "goes somewhere".
export const AsLink: Story = { args: { href: '/nodes', children: 'Coverage report' } }

export const Row: Story = {
  render: () => (
    <div className="row">
      <Button primary>Save changes</Button>
      <Button>Test</Button>
      <Button subtle>Cancel</Button>
      <Button danger>Delete node</Button>
    </div>
  ),
}
