import { useLocation } from 'react-router-dom'
import { Button, EmptyPrompt } from '../components/ui'

// Ported from internal/web/templates/notfound.html (docs/adr/0017, Phase 8).
// Rendered inside AppShell by the router's wildcard route — a real 404
// status comes from internal/web's notFound handler ahead of this
// (w.WriteHeader before serving the SPA shell), this just names the path.
export function NotFoundPage() {
  const { pathname } = useLocation()
  return (
    <div style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center', padding: 24 }}>
      <EmptyPrompt
        title="Not found"
        body={
          <>
            <div>There is no page at <span className="m">{pathname}</span>.</div>
            <div style={{ marginTop: 12 }}>
              <Button primary href="/">Back to the dashboard</Button>
            </div>
          </>
        }
      />
    </div>
  )
}
