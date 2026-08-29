import { useState } from 'react'
import {
  EuiBadge,
  EuiBasicTable,
  EuiFlexGrid,
  EuiFlexGroup,
  EuiFlexItem,
  EuiIcon,
  EuiLink,
  EuiLoadingChart,
  EuiPageTemplate,
  EuiPanel,
  EuiSelect,
  EuiSpacer,
  EuiStat,
  EuiText,
  EuiTitle,
} from '@elastic/eui'
import { useDashboard } from '../../api/queries/dashboard'
import type { DashboardRiskCluster } from '../../api/pb/nagipath/api/v1/dashboard_pb'

const toneColor: Record<string, 'success' | 'warning' | 'danger' | 'default'> = {
  ok: 'success',
  deg: 'warning',
  err: 'danger',
  inf: 'default',
}

// ActivityStrip is internal/web's 24-hour bar strip, ported as plain divs —
// the original deliberately carried no chart library (dashboard.go: "There
// is no chart library and no axis — the count is printed beside the shape"),
// and that reasoning still applies here.
function ActivityStrip({ buckets, max }: { buckets: { label: string; total: number; failed: number; degraded: number }[]; max: number }) {
  return (
    <EuiFlexGroup gutterSize="xs" alignItems="flexEnd" style={{ height: 60 }}>
      {buckets.map((b) => {
        const height = max > 0 ? Math.max(2, (b.total / max) * 56) : 2
        const color = b.failed > 0 ? '#a1231c' : b.degraded > 0 ? '#f5a700' : '#0077cc'
        return (
          <EuiFlexItem key={b.label} grow={false} title={`${b.label} — ${b.total}`}>
            <div style={{ width: 6, height, background: b.total > 0 ? color : '#eef1f7', borderRadius: 2 }} />
          </EuiFlexItem>
        )
      })}
    </EuiFlexGroup>
  )
}

// Dashboard is the fleet-overview screen — ported from internal/web's
// dashboard.html against GET /api/ui/dashboard (internal/api/dashboard.go),
// same filters (cluster scope, attention severity/cluster) and same fields.
export function Dashboard() {
  const [cluster, setCluster] = useState(0)
  const [severity, setSeverity] = useState('all')
  const { data, isPending, isError, error } = useDashboard({ cluster, severity })

  if (isPending) {
    return (
      <EuiPageTemplate.EmptyPrompt icon={<EuiLoadingChart size="xl" />} title={<h2>Loading dashboard…</h2>} />
    )
  }
  if (isError) {
    return <EuiPageTemplate.EmptyPrompt iconType="alert" color="danger" title={<h2>Could not load the dashboard</h2>} body={<p>{error.message}</p>} />
  }

  const riskColumns = [
    { field: 'name', name: 'Cluster' },
    { field: 'freshPercent', name: 'Fresh', render: (v: number) => `${v}%` },
    { field: 'drift', name: 'Drift' },
    { field: 'certsLabel', name: 'Certs' },
    {
      field: 'state',
      name: 'State',
      render: (state: string) => (
        <EuiBadge color={state === 'ok' ? 'success' : state === 'failing' ? 'danger' : state === 'drift' ? 'warning' : 'default'}>
          {state}
        </EuiBadge>
      ),
    },
  ]

  return (
    <>
      <EuiFlexGroup alignItems="center" justifyContent="spaceBetween">
        <EuiFlexItem grow={false}>
          <EuiTitle size="m">
            <h1>Dashboard</h1>
          </EuiTitle>
        </EuiFlexItem>
        <EuiFlexItem grow={false}>
          <EuiSelect
            compressed
            options={[{ value: '0', text: 'All clusters' }, ...data.clusters.map((c) => ({ value: c.id.toString(), text: c.name }))]}
            value={String(cluster)}
            onChange={(e) => setCluster(Number(e.target.value))}
          />
        </EuiFlexItem>
      </EuiFlexGroup>
      <EuiSpacer />

      <EuiFlexGrid columns={4}>
        <EuiFlexItem>
          <EuiPanel>
            <EuiStat title={data.nodes} description="Nodes" titleColor="primary" />
          </EuiPanel>
        </EuiFlexItem>
        <EuiFlexItem>
          <EuiPanel>
            <EuiStat title={data.instances} description="Instances" titleColor="primary" />
          </EuiPanel>
        </EuiFlexItem>
        <EuiFlexItem>
          <EuiPanel>
            <EuiStat title={data.driftedInstances} description="Drifted instances" titleColor={data.driftedInstances > 0 ? 'warning' : 'primary'} />
          </EuiPanel>
        </EuiFlexItem>
        <EuiFlexItem>
          <EuiPanel>
            <EuiStat title={data.certsExpiring30d} description="Certs expiring (30d)" titleColor={data.certsExpiring30d > 0 ? 'warning' : 'primary'} />
          </EuiPanel>
        </EuiFlexItem>
        <EuiFlexItem>
          <EuiPanel>
            <EuiStat title={data.unreachableNodes} description="Unreachable nodes" titleColor={data.unreachableNodes > 0 ? 'danger' : 'primary'} />
          </EuiPanel>
        </EuiFlexItem>
        <EuiFlexItem>
          <EuiPanel>
            <EuiStat title={data.pendingHostKeys} description="Pending host keys" titleColor={data.pendingHostKeys > 0 ? 'warning' : 'primary'} />
          </EuiPanel>
        </EuiFlexItem>
        <EuiFlexItem>
          <EuiPanel>
            <EuiStat title={data.totalRules} description="Rules in force" titleColor="primary" />
          </EuiPanel>
        </EuiFlexItem>
        <EuiFlexItem>
          <EuiPanel>
            <EuiStat title={data.vendorCount} description="Vendors" titleColor="primary" />
          </EuiPanel>
        </EuiFlexItem>
      </EuiFlexGrid>

      <EuiSpacer size="l" />

      <EuiFlexGroup>
        <EuiFlexItem grow={2}>
          <EuiPanel>
            <EuiFlexGroup alignItems="center" justifyContent="spaceBetween">
              <EuiFlexItem grow={false}>
                <EuiTitle size="xs">
                  <h2>Needs attention</h2>
                </EuiTitle>
              </EuiFlexItem>
              <EuiFlexItem grow={false}>
                <EuiSelect
                  compressed
                  options={[
                    { value: 'all', text: 'All severities' },
                    { value: 'err', text: 'Error' },
                    { value: 'deg', text: 'Degraded' },
                    { value: 'inf', text: 'Info' },
                  ]}
                  value={severity}
                  onChange={(e) => setSeverity(e.target.value)}
                />
              </EuiFlexItem>
            </EuiFlexGroup>
            <EuiSpacer size="s" />
            {data.attention.length === 0 ? (
              <EuiText color="subdued" size="s">
                Nothing needs attention.
              </EuiText>
            ) : (
              data.attention.map((a, i) => (
                <div key={i} style={{ padding: '6px 0', borderBottom: '1px solid #edf0f5' }}>
                  <EuiFlexGroup gutterSize="s" alignItems="center">
                    <EuiFlexItem grow={false}>
                      <EuiBadge color={toneColor[a.tone] ?? 'default'}>{a.kind}</EuiBadge>
                    </EuiFlexItem>
                    <EuiFlexItem>
                      <EuiLink href={a.link}>{a.text}</EuiLink>
                      {a.note && (
                        <EuiText size="xs" color="subdued">
                          {a.note}
                        </EuiText>
                      )}
                    </EuiFlexItem>
                  </EuiFlexGroup>
                </div>
              ))
            )}
          </EuiPanel>
        </EuiFlexItem>

        <EuiFlexItem grow={1}>
          <EuiPanel>
            <EuiTitle size="xs">
              <h2>Collection activity (24h)</h2>
            </EuiTitle>
            <EuiSpacer size="s" />
            <ActivityStrip buckets={data.activity} max={data.activityMax} />
          </EuiPanel>
          <EuiSpacer />
          <EuiPanel>
            <EuiTitle size="xs">
              <h2>
                <EuiIcon type="storage" /> Cluster risk
              </h2>
            </EuiTitle>
            <EuiSpacer size="s" />
            <EuiBasicTable<DashboardRiskCluster>
              items={data.riskClusters}
              columns={riskColumns}
              rowHeader="name"
              noItemsMessage="No clusters yet."
            />
          </EuiPanel>
        </EuiFlexItem>
      </EuiFlexGroup>
    </>
  )
}
