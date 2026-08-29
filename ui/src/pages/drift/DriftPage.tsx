import {
  EuiBadge,
  EuiBasicTable,
  EuiButton,
  EuiFlexGroup,
  EuiFlexItem,
  EuiLoadingChart,
  EuiPageTemplate,
  EuiPanel,
  EuiSelect,
  EuiSpacer,
  EuiStat,
  EuiText,
  EuiTitle,
} from '@elastic/eui'
import { useSearchParams } from 'react-router-dom'
import { useDrift, useRecomputeDrift } from '../../api/queries/drift'
import { useSession } from '../../api/queries/session'
import type { DriftInstanceRow } from '../../api/pb/nagipath/api/v1/drift_pb'

// Ported from internal/web/templates/drift.html against GET/POST
// /api/ui/drift (internal/api/drift.go) — same scope/baseline/object-kind
// filters, same cluster-grouped work queue.
export function DriftPage() {
  const [params, setParams] = useSearchParams()
  const { data: session } = useSession()
  const cluster = params.get('cluster') ?? ''
  const scope = params.get('scope') ?? ''
  const { data, isPending, isError, error } = useDrift({ cluster, scope })
  const recompute = useRecomputeDrift()

  if (isPending) return <EuiPageTemplate.EmptyPrompt icon={<EuiLoadingChart size="xl" />} title={<h2>Loading drift…</h2>} />
  if (isError) return <EuiPageTemplate.EmptyPrompt iconType="alert" color="danger" title={<h2>Could not load drift</h2>} body={<p>{error.message}</p>} />

  const isAdmin = session?.user?.role === 'admin'

  const columns = [
    {
      field: 'displayName',
      name: 'Node',
      render: (name: string, row: DriftInstanceRow) => (
        <>
          <a href={`/instances/${row.id}`}>{name}</a>
          {row.nodeDisplayName && <div><EuiText size="xs" color="subdued">{row.nodeDisplayName}</EuiText></div>}
        </>
      ),
    },
    { field: 'divergenceCount', name: 'Divergences', render: (n: number) => <EuiBadge color="warning">{n}</EuiBadge> },
    { field: 'objectBreakdown', name: 'Objects' },
    { field: 'firstSeen', name: 'First seen', render: (v: string) => (v ? new Date(v).toLocaleString() : '—') },
    {
      name: 'Review',
      render: (row: DriftInstanceRow) => <EuiButton size="s" href={`/drift/review/${row.id}`}>Review</EuiButton>,
    },
  ]

  return (
    <>
      <EuiFlexGroup alignItems="center" justifyContent="spaceBetween">
        <EuiFlexItem grow={false}>
          <EuiTitle size="m"><h1>Drift</h1></EuiTitle>
        </EuiFlexItem>
        {isAdmin && (
          <EuiFlexItem grow={false}>
            <EuiButton
              size="s"
              isLoading={recompute.isPending}
              onClick={() => recompute.mutate({ cluster: data.scope, baseline: data.baseline })}
            >
              Re-diff {data.allScope ? 'every instance' : 'cluster'}
            </EuiButton>
          </EuiFlexItem>
        )}
      </EuiFlexGroup>
      <EuiSpacer />

      <EuiFlexGroup gutterSize="s">
        <EuiFlexItem grow={false}>
          <EuiSelect
            options={[
              ...data.clusters.map((c) => ({ value: c.id.toString(), text: `${c.name} · ${c.members} instance(s)` })),
              { value: 'all', text: 'Every instance · vs previous snapshot' },
            ]}
            value={cluster || (data.allScope ? 'all' : data.scope)}
            onChange={(e) => setParams((p) => { p.set('cluster', e.target.value); return p })}
          />
        </EuiFlexItem>
        <EuiFlexItem grow={false}>
          <EuiSelect
            options={[
              { value: '', text: 'Scope: all objects' },
              { value: 'route', text: 'Scope: routes' },
              { value: 'upstream', text: 'Scope: upstreams' },
              { value: 'site', text: 'Scope: sites' },
              { value: 'listener', text: 'Scope: listeners' },
              { value: 'rule', text: 'Scope: rules' },
            ]}
            value={scope}
            onChange={(e) => setParams((p) => { if (e.target.value) p.set('scope', e.target.value); else p.delete('scope'); return p })}
          />
        </EuiFlexItem>
        <EuiFlexItem grow={false}>
          <EuiText size="s" color="subdued">
            {data.instancesWithDrift.length} of {data.memberCount} nodes differ
            {data.clusters.length > 0 && ` · ${data.clusters.length} cluster(s)`}
          </EuiText>
        </EuiFlexItem>
      </EuiFlexGroup>
      <EuiSpacer />

      {data.empty ? (
        <EuiPageTemplate.EmptyPrompt
          title={<h2>Nothing is collected yet</h2>}
          body={<p>Drift compares parsed configuration between instances, and there is none. <a href="/nodes/import">Import inventory</a> first.</p>}
        />
      ) : (
        <>
          <EuiFlexGroup>
            <EuiFlexItem grow={false}><EuiPanel><EuiStat title={data.instancesWithDrift.length} description="Nodes off baseline" titleColor={data.instancesWithDrift.length > 0 ? 'warning' : 'primary'} /></EuiPanel></EuiFlexItem>
            <EuiFlexItem grow={false}><EuiPanel><EuiStat title={data.totalDivergentObjects} description="Divergent objects" titleColor="primary" /></EuiPanel></EuiFlexItem>
            <EuiFlexItem grow={false}><EuiPanel><EuiStat title={data.clustersWithoutBaseline} description="No baseline set" titleColor={data.clustersWithoutBaseline > 0 ? 'warning' : 'primary'} /></EuiPanel></EuiFlexItem>
          </EuiFlexGroup>
          <EuiSpacer />

          {data.runs.length === 0 ? (
            <EuiPanel>
              <EuiTitle size="xs"><h2>Nothing compared yet</h2></EuiTitle>
              <EuiSpacer size="s" />
              <EuiText size="s" color="subdued">
                Re-diff compares {data.allScope ? 'every instance' : 'every member of this cluster'} against its baseline.
              </EuiText>
              {isAdmin && (
                <>
                  <EuiSpacer size="s" />
                  <EuiButton size="s" isLoading={recompute.isPending} onClick={() => recompute.mutate({ cluster: data.scope })}>
                    Re-diff now
                  </EuiButton>
                </>
              )}
            </EuiPanel>
          ) : data.instancesWithDrift.length === 0 ? (
            <EuiPanel>
              <EuiTitle size="xs"><h2>No divergences</h2></EuiTitle>
              <EuiSpacer size="s" />
              <EuiText size="s" color="subdued">Every member matches its baseline object for object.</EuiText>
            </EuiPanel>
          ) : (
            <EuiPanel>
              {data.clusterGroups.map((g) => (
                <div key={g.clusterName}>
                  <EuiText size="s"><strong>{g.clusterName}</strong>{' '}
                    <span style={{ color: '#69707d' }}>{g.baselineName && `baseline ${g.baselineName} · `}{g.nodesWithDrift} of {g.totalNodes} nodes differ</span>
                  </EuiText>
                  <EuiBasicTable<DriftInstanceRow> items={g.instances} columns={columns} rowHeader="displayName" />
                  <EuiSpacer size="m" />
                </div>
              ))}
            </EuiPanel>
          )}
        </>
      )}
    </>
  )
}
