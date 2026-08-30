import type { Meta, StoryObj } from '@storybook/react-vite'
import { navIcons, type NavIconName } from './icons'

// The whole set.
const meta: Meta = {
  title: 'Components/Icons',
  parameters: {
    docs: {
      description: {
        component:
          '19 hand-drawn monoline icons (24×24, `stroke=currentColor`, no icon font, no CDN). Referenced by name through `navIcons[name]`, which is what `navConfig.ts` stores — so a nav item can never point at an icon that does not exist.',
      },
    },
  },
}
export default meta

export const All: StoryObj = {
  render: () => (
    <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(120px, 1fr))', gap: 12 }}>
      {(Object.keys(navIcons) as NavIconName[]).map((name) => {
        const Icon = navIcons[name]
        return (
          <div key={name} className="pnl col" style={{ alignItems: 'center', gap: 6, padding: 12 }}>
            <span className="ic" style={{ width: 24, height: 24, color: '#1a1c21' }}><Icon /></span>
            <span className="m mus">{name}</span>
          </div>
        )
      })}
    </div>
  ),
}

// Icons inherit colour and are sized by their container — the sidebar's 16px
// `.ic`, a page header's 20px. No per-icon size or colour props.
export const Sizes: StoryObj = {
  render: () => (
    <div className="row" style={{ alignItems: 'center', color: '#0b64dd' }}>
      {[16, 20, 24, 32].map((px) => {
        const Icon = navIcons.drift
        return <span key={px} style={{ width: px, height: px, display: 'inline-flex' }}><Icon /></span>
      })}
    </div>
  ),
}
