import { useState } from 'react'
import type { Meta, StoryObj } from '@storybook/react-vite'
import { Resizer } from './Resizer'

const meta: Meta<typeof Resizer> = {
  title: 'Components/Resizer',
  component: Resizer,
  tags: ['autodocs'],
  parameters: {
    docs: {
      description: {
        component:
          "The draggable seam between two panes of a Finder-style cascade — the node detail Routes tab (screen 5a) uses one per column. It owns no width itself: the parent holds the number and the Resizer reports a new one, so a page can clamp, persist or share it. Drag it, or focus it and use ← / →.",
      },
    },
  },
}
export default meta

type Story = StoryObj<typeof meta>

export const Cascade: Story = {
  render: () => {
    const [w, setW] = useState(200)
    return (
      <div style={{ display: 'flex', height: 180, border: '1px solid #d3dae6', borderRadius: 4 }}>
        <div style={{ flex: `0 0 ${w}px`, minWidth: 0, padding: 8 }}>
          <span className="m mus">{w}px — drag the seam →</span>
        </div>
        <Resizer width={w} onChange={setW} />
        <div style={{ flex: 1, minWidth: 0, padding: 8 }}>
          <span className="m mus">fills the rest</span>
        </div>
      </div>
    )
  },
}
