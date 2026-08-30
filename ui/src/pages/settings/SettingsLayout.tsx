import type { ReactNode } from 'react'
import { Link, useLocation } from 'react-router-dom'
import { useDiagnostics } from '../../api/queries/settings'
import { useSession } from '../../api/queries/session'
import { PageHeader, Panel, navIcons } from '../../components/ui'
import { settingsItems } from '../../components/layout/navConfig'


// One wrapper for every Settings sub-page's chrome: the fixed left sub-nav
// (design/'s screens 8a-8h all share it) plus the "This install" panel
// design/ pins to the bottom of that sidebar on every one of those screens.
// Reuses SystemPage's existing diagnostics query rather than a new endpoint;
// gated to admins since that query already is (System is an admin-only nav
// item) so viewers on Audit log/License don't fire a request they'd get a
// 403 from.
export function SettingsLayout({ title, actions, children }: { title: ReactNode; actions?: ReactNode; children: ReactNode }) {
  const { data: session } = useSession()
  const isAdmin = session?.user?.role === 'admin'
  const { pathname } = useLocation()
  const { data: diag } = useDiagnostics({ enabled: isAdmin })

  return (
    <>
      <PageHeader title={title} actions={actions} />
      <div className="bd">
        <div className="row" style={{ alignItems: 'flex-start' }}>
          <div style={{ flex: '0 0 200px' }}>
            <Panel style={{ padding: 8 }}>
              {settingsItems
                .filter((it) => !it.admin || isAdmin)
                .map((it) => {
                  const Icon = navIcons[it.icon]
                  return (
                    <Link key={it.href} to={it.href} className={`ni${pathname === it.href ? ' on' : ''}`}>
                      <span className="ic"><Icon /></span>{it.label}
                    </Link>
                  )
                })}
            </Panel>
            {diag && (
              <div style={{ marginTop: 20 }}>
                <Panel>
                  <div className="m mus">This install</div>
                  <div className="m" style={{ marginTop: 4 }}>nagipath {diag.version}</div>
                  <div className="m mus">
                    up {Math.floor(Number(diag.uptimeSeconds) / 3600)}h · {(Number(diag.dbSizeBytes) / 1e6).toFixed(1)} MB
                  </div>
                </Panel>
              </div>
            )}
          </div>
          <div style={{ flex: 1, minWidth: 0 }}>{children}</div>
        </div>
      </div>
    </>
  )
}
