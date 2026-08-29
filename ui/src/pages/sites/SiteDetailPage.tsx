import {
  EuiBadge,
  EuiBasicTable,
  EuiFlexGroup,
  EuiFlexItem,
  EuiLoadingChart,
  EuiPageTemplate,
  EuiPanel,
  EuiSpacer,
  EuiTabbedContent,
  EuiText,
  EuiTitle,
} from '@elastic/eui'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { useSite } from '../../api/queries/sites'
import type { SiteDetailRoute, SiteNodeRow, SiteUpstreamMember } from '../../api/pb/nagipath/api/v1/sites_pb'

// Ported from internal/web/templates/site.html against
// GET /api/ui/sites/{name} (internal/api/sites.go). The path-graph diagram
// the old page shared with Trace is deferred to Phase 6, alongside Trace
// itself — this page covers the four data tabs without it.
export function SiteDetailPage() {
  const { name = '' } = useParams()
  const [params, setParams] = useSearchParams()
  const navigate = useNavigate()
  const tab = params.get('tab') ?? 'overview'
  const variant = params.get('variant') ?? undefined

  const { data, isPending, isError, error } = useSite(name, { tab, variant })

  if (isPending) return <EuiPageTemplate.EmptyPrompt icon={<EuiLoadingChart size="xl" />} title={<h2>Loading site…</h2>} />
  if (isError) {
    const notFound = error.message?.toLowerCase().includes('no node')
    return (
      <EuiPageTemplate.EmptyPrompt
        iconType={notFound ? 'search' : 'alert'}
        color={notFound ? 'subdued' : 'danger'}
        title={<h2>{notFound ? 'No such site' : 'Could not load this site'}</h2>}
        body={<p>{error.message}</p>}
      />
    )
  }

  return (
    <>
      <EuiTitle size="m">
        <h1>{data.name}</h1>
      </EuiTitle>
      <EuiSpacer />

      <EuiTabbedContent
        selectedTab={{ id: tab, name: '', content: null }}
        onTabClick={(t) => setParams((p) => { if (t.id === 'overview') p.delete('tab'); else p.set('tab', t.id); return p })}
        tabs={data.tabs.map((t) => ({
          id: t.href.includes('tab=') ? new URLSearchParams(t.href.split('?')[1]).get('tab')! : 'overview',
          name: t.count > 0 ? `${t.label} (${t.count})` : t.label,
          content: null,
        }))}
      />
      <EuiSpacer />

      {tab === 'overview' && data.overview && (
        <EuiFlexGroup>
          <EuiFlexItem grow={2}>
            <EuiPanel>
              <EuiTitle size="xs"><h2>Routes</h2></EuiTitle>
              <EuiSpacer size="s" />
              <EuiBasicTable<SiteDetailRoute>
                items={data.overview.routes}
                columns={[
                  { field: 'pattern', name: 'Pattern' },
                  { field: 'matchType', name: 'Match' },
                  { field: 'action', name: 'Action' },
                  { field: 'target', name: 'Target' },
                ]}
                noItemsMessage="No routes."
              />
            </EuiPanel>
          </EuiFlexItem>
          <EuiFlexItem grow={1}>
            <EuiPanel>
              <EuiText size="s">
                <p>listener: {data.overview.listenerSummary}</p>
                <p>certificate: {data.overview.certSubject || 'none'} {data.overview.certExpiry && `· expires in ${data.overview.certExpiryDays}d`}</p>
                <p>upstreams: {data.overview.upstreams.length}</p>
              </EuiText>
            </EuiPanel>
            <EuiSpacer />
            <EuiPanel>
              <EuiTitle size="xs"><h2>Nodes serving this site</h2></EuiTitle>
              <EuiSpacer size="s" />
              {data.nodes.map((n) => (
                <EuiFlexGroup key={n.nodeId.toString()} gutterSize="s" style={{ padding: '4px 0' }}>
                  <EuiFlexItem>{n.nodeName}</EuiFlexItem>
                  <EuiFlexItem grow={false}><EuiBadge>{n.variant}</EuiBadge></EuiFlexItem>
                </EuiFlexGroup>
              ))}
            </EuiPanel>
          </EuiFlexItem>
        </EuiFlexGroup>
      )}

      {tab === 'nodes' && (
        <EuiPanel>
          <EuiBasicTable<SiteNodeRow>
            items={data.nodes}
            columns={[
              { field: 'nodeName', name: 'Node' },
              { field: 'cluster', name: 'Cluster' },
              { field: 'variant', name: 'Variant' },
              { field: 'listener', name: 'Listener' },
              { field: 'state', name: 'State', render: (s: string) => <EuiBadge color={s === 'OK' ? 'success' : 'warning'}>{s}</EuiBadge> },
            ]}
            noItemsMessage="No nodes."
          />
        </EuiPanel>
      )}

      {tab === 'upstreams' && (
        <EuiPanel>
          <EuiBasicTable<SiteUpstreamMember>
            items={data.upstreams}
            columns={[
              { field: 'upstream', name: 'Upstream' },
              { field: 'host', name: 'Host' },
              { field: 'port', name: 'Port' },
              { field: 'nodeName', name: 'Node' },
            ]}
            noItemsMessage="No upstreams."
          />
        </EuiPanel>
      )}

      {tab === 'certificates' && (
        <EuiPanel>
          <EuiTitle size="xs"><h2>Certificates bound</h2></EuiTitle>
          <EuiSpacer size="s" />
          {data.certs.length === 0 ? (
            <EuiText size="s" color="subdued">No certificates bound to this site.</EuiText>
          ) : (
            data.certs.map((c, i) => (
              <div key={i} style={{ padding: '6px 0', borderBottom: '1px solid #edf0f5' }}>
                <EuiText size="s"><strong>{c.subject}</strong> · expires in {c.expiryDays}d · {c.bindings} binding(s)</EuiText>
              </div>
            ))
          )}
        </EuiPanel>
      )}

      <EuiSpacer />
      {data.variantOpts.length > 0 && (
        <EuiFlexGroup gutterSize="xs">
          {data.variantOpts.map((v) => (
            <EuiFlexItem grow={false} key={v.key}>
              <EuiBadge
                color={v.on ? 'primary' : 'hollow'}
                onClick={() => setParams((p) => { p.set('variant', v.key); return p })}
                onClickAriaLabel={v.label}
              >
                {v.label}
              </EuiBadge>
            </EuiFlexItem>
          ))}
        </EuiFlexGroup>
      )}
      <EuiSpacer size="s" />
      <EuiText size="xs" color="subdued" onClick={() => navigate('/sites')} style={{ cursor: 'pointer' }}>
        ← Back to sites
      </EuiText>
    </>
  )
}
