import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import { CodeEditor } from './CodeEditor'

const sample = `server {
  listen 443 ssl;
  server_name shop.example.com;
  location / {
    proxy_pass http://shop_app;
  }
}`

const meta: Meta<typeof CodeEditor> = {
  title: 'Components/CodeEditor',
  component: CodeEditor,
  tags: ['autodocs'],
  parameters: {
    docs: {
      description: {
        component:
          'The editable twin of CodeBlock, used by Node detail › Config files when an admin edits the live file on the node. Uncoloured and ungutter-ed on purpose: the read view keeps both, the edit view keeps the text honest.',
      },
    },
  },
  render: function Render(args) {
    const [value, setValue] = useState(args.value)
    return (
      <div style={{ height: 220, display: 'flex' }}>
        <CodeEditor {...args} value={value} onChange={setValue} />
      </div>
    )
  },
  args: { value: sample },
}
export default meta

type Story = StoryObj<typeof meta>

export const Default: Story = {}

// While a save is in flight the field is frozen rather than unmounted, so the
// operator can still see what they are about to write.
export const Saving: Story = { args: { disabled: true } }
