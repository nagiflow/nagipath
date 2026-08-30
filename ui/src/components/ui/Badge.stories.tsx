import type { Meta, StoryObj } from '@storybook/react-vite'
import { Badge, badgeClass } from './Badge'

// The state words the app actually puts in a badge, grouped by the colour
// bucket badgeClass maps them to. A new state word without a bucket falls
// back to `.n` (blue) — add it to bucketClass in Badge.tsx rather than passing
// `cls` at the call site, so two pages can't disagree about its colour.
const buckets: Record<string, string[]> = {
  'v · verified / good': ['verified', 'conforming', 'succeeded', 'approved', 'valid', 'ok', 'parsed', 'completed'],
  'i · inferred / not known': ['inferred', 'candidate', 'missing'],
  'd · needs attention': ['observed_effect', 'partial', 'degraded', 'expiring', 'stale', 'pending', 'drift', 'over_ceiling', 'running'],
  'e · left the fleet': ['external_hop'],
  'r · failed / refused': ['disproved', 'failed', 'expired', 'quarantined', 'denied', 'rejected', 'blocked'],
  'n · neutral (unmapped)': ['manual', 'schedule'],
}

const meta: Meta<typeof Badge> = {
  title: 'Components/Badge',
  component: Badge,
  tags: ['autodocs'],
  args: { state: 'verified' },
  parameters: {
    docs: {
      description: {
        component: "design/'s `.bg` pill. The colour is a function of the state word, not of the caller — six buckets, no free-form colours.",
      },
    },
  },
}
export default meta

type Story = StoryObj<typeof meta>

export const Default: Story = {}

export const AllStates: Story = {
  render: () => (
    <div className="col">
      {Object.entries(buckets).map(([label, states]) => (
        <div key={label} className="col" style={{ gap: 4 }}>
          <span className="lbl">{label}</span>
          <div className="row" style={{ flexWrap: 'wrap', gap: 6 }}>
            {states.map((s) => <Badge key={s} state={s} title={`.${badgeClass(s)}`} />)}
          </div>
        </div>
      ))}
    </div>
  ),
}

// A count or a phrase instead of the state word, with the bucket named
// explicitly — the dashboard's tone codes and table state columns do this.
export const ExplicitBucket: Story = {
  render: () => (
    <div className="row">
      <Badge cls="v">1,092 OK</Badge>
      <Badge cls="d">31 DEGRADED</Badge>
      <Badge cls="r">6 FAILED</Badge>
      <Badge cls="d">3 VARIANTS</Badge>
    </div>
  ),
}
