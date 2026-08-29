import { EuiEmptyPrompt, EuiLoadingLogo, EuiPageTemplate, EuiText } from '@elastic/eui'
import { useSession } from '../../api/queries/session'

// Temporary Phase-0 landing page: not part of the app's IA, deleted once
// Phase 1 ships the real Dashboard + AppShell at "/".
export function FoundationCheck() {
  const { data, isPending, isError, error } = useSession()

  if (isPending) {
    return (
      <EuiPageTemplate>
        <EuiPageTemplate.EmptyPrompt icon={<EuiLoadingLogo logo="logoElastic" size="xl" />} title={<h2>Loading session…</h2>} />
      </EuiPageTemplate>
    )
  }

  if (isError) {
    return (
      <EuiPageTemplate>
        <EuiPageTemplate.EmptyPrompt
          iconType="alert"
          color="danger"
          title={<h2>Session bootstrap failed</h2>}
          body={<p>{error.message}</p>}
        />
      </EuiPageTemplate>
    )
  }

  return (
    <EuiPageTemplate>
      <EuiEmptyPrompt
        iconType="check"
        title={<h2>SPA foundation is wired up</h2>}
        body={
          <EuiText>
            <p>
              Signed in as <strong>{data.user.username}</strong> ({data.user.role}).
            </p>
            <p>{data.nav_counts.nodes} nodes, {data.nav_counts.drift} with unignored drift.</p>
          </EuiText>
        }
      />
    </EuiPageTemplate>
  )
}
