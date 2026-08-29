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
import type { SiteDetailRoute, SiteNodeRow, SiteUpstreamMember } from '../../api/types'

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
        <h1>{data.Name}</h1>
      </EuiTitle>
      <EuiSpacer />

      <EuiTabbedContent
        selectedTab={{ id: tab, name: '', content: null }}
        onTabClick={(t) => setParams((p) => { if (t.id === 'overview') p.delete('tab'); else p.set('tab', t.id); return p })}
        tabs={data.Tabs.map((t) => ({
          id: t.Href.includes('tab=') ? new URLSearchParams(t.Href.split('?')[1]).get('tab')! : 'overview',
          name: t.Count > 0 ? `${t.Label} (${t.Count})` : t.Label,
          content: null,
        }))}
      />
      <EuiSpacer />

      {tab === 'overview' && data.Overview && (
        <EuiFlexGroup>
          <EuiFlexItem grow={2}>
            <EuiPanel>
              <EuiTitle size="xs"><h2>Routes</h2></EuiTitle>
              <EuiSpacer size="s" />
              <EuiBasicTable<SiteDetailRoute>
                items={data.Overview.Routes}
                columns={[
                  { field: 'Pattern', name: 'Pattern' },
                  { field: 'MatchType', name: 'Match' },
                  { field: 'Action', name: 'Action' },
                  { field: 'Target', name: 'Target' },
                ]}
                noItemsMessage="No routes."
              />
            </EuiPanel>
          </EuiFlexItem>
          <EuiFlexItem grow={1}>
            <EuiPanel>
              <EuiText size="s">
                <p>listener: {data.Overview.ListenerSummary}</p>
                <p>certificate: {data.Overview.CertSubject || 'none'} {data.Overview.CertExpiry && `· expires in ${data.Overview.CertExpiryDays}d`}</p>
                <p>upstreams: {data.Overview.Upstreams.length}</p>
              </EuiText>
            </EuiPanel>
            <EuiSpacer />
            <EuiPanel>
              <EuiTitle size="xs"><h2>Nodes serving this site</h2></EuiTitle>
              <EuiSpacer size="s" />
              {(data.Nodes ?? []).map((n) => (
                <EuiFlexGroup key={n.NodeID} gutterSize="s" style={{ padding: '4px 0' }}>
                  <EuiFlexItem>{n.NodeName}</EuiFlexItem>
                  <EuiFlexItem grow={false}><EuiBadge>{n.Variant}</EuiBadge></EuiFlexItem>
                </EuiFlexGroup>
              ))}
            </EuiPanel>
          </EuiFlexItem>
        </EuiFlexGroup>
      )}

      {tab === 'nodes' && (
        <EuiPanel>
          <EuiBasicTable<SiteNodeRow>
            items={data.Nodes ?? []}
            columns={[
              { field: 'NodeName', name: 'Node' },
              { field: 'Cluster', name: 'Cluster' },
              { field: 'Variant', name: 'Variant' },
              { field: 'Listener', name: 'Listener' },
              { field: 'State', name: 'State', render: (s: string) => <EuiBadge color={s === 'OK' ? 'success' : 'warning'}>{s}</EuiBadge> },
            ]}
            noItemsMessage="No nodes."
          />
        </EuiPanel>
      )}

      {tab === 'upstreams' && (
        <EuiPanel>
          <EuiBasicTable<SiteUpstreamMember>
            items={data.Upstreams ?? []}
            columns={[
              { field: 'Upstream', name: 'Upstream' },
              { field: 'Host', name: 'Host' },
              { field: 'Port', name: 'Port' },
              { field: 'NodeName', name: 'Node' },
            ]}
            noItemsMessage="No upstreams."
          />
        </EuiPanel>
      )}

      {tab === 'certificates' && (
        <EuiPanel>
          <EuiTitle size="xs"><h2>Certificates bound</h2></EuiTitle>
          <EuiSpacer size="s" />
          {(data.Certs ?? []).length === 0 ? (
            <EuiText size="s" color="subdued">No certificates bound to this site.</EuiText>
          ) : (
            data.Certs!.map((c, i) => (
              <div key={i} style={{ padding: '6px 0', borderBottom: '1px solid #edf0f5' }}>
                <EuiText size="s"><strong>{c.Subject}</strong> · expires in {c.ExpiryDays}d · {c.Bindings} binding(s)</EuiText>
              </div>
            ))
          )}
        </EuiPanel>
      )}

      <EuiSpacer />
      {data.VariantOpts.length > 0 && (
        <EuiFlexGroup gutterSize="xs">
          {data.VariantOpts.map((v) => (
            <EuiFlexItem grow={false} key={v.Key}>
              <EuiBadge
                color={v.On ? 'primary' : 'hollow'}
                onClick={() => setParams((p) => { p.set('variant', v.Key); return p })}
                onClickAriaLabel={v.Label}
              >
                {v.Label}
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
