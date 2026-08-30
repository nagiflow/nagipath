import { useEffect, useRef, useState, type ReactNode } from 'react'
import { Link, Navigate, useLocation, useNavigate } from 'react-router-dom'
import { useSession } from '../../api/queries/session'
import { logout } from '../../lib/auth'
import { breadcrumbFor, navGroups, settingsHref } from './navConfig'
import { EmptyPrompt, Loading, navIcons, SettingsIcon } from '../ui'

// design/'s header search box ("Search hosts, rules, certificates" · ⌘K) —
// routes into the existing Config search page rather than a new search
// engine; ⌘K/Ctrl+K focuses it from anywhere in the app.
function GlobalSearch() {
  const navigate = useNavigate()
  const ref = useRef<HTMLInputElement | null>(null)
  const [value, setValue] = useState('')

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
        e.preventDefault()
        ref.current?.focus()
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [])

  return (
    <div className="gsearch">
      <input
        ref={ref}
        placeholder="Search hosts, rules, certificates"
        value={value}
        onChange={(e) => setValue(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter' && value.trim()) navigate(`/search?q=${encodeURIComponent(value.trim())}`)
        }}
      />
      <span style={{ flex: 1 }} />
      <span className="kbd">⌘K</span>
    </div>
  )
}

// AppShell is the SPA's persistent chrome (docs/adr/0017): sidebar and
// header — the same shell every ported page renders inside, driven
// entirely by GET /api/session the way internal/web/nav.go's one table
// used to drive layout.html's sidebar + breadcrumb. Renders design/'s literal
// .hdr/.nav/.wrap/.main markup (ui/src/theme/theme.css) rather than an
// EUI approximation of it.
export function AppShell({ children }: { children: ReactNode }) {
  const { data: session, isPending, isError, error } = useSession()
  const { pathname } = useLocation()
  const crumb = breadcrumbFor(pathname)

  if (isPending) return <Loading label="Loading…" />
  if (isError) return <EmptyPrompt danger title="Could not load session" body={error.message} />

  // GET /session is public now (Login/Setup need it before any session
  // exists), so it no longer 401s an anonymous caller into the branch above.
  // A full page load never reaches this — internal/web's auth() middleware
  // still redirects to /login server-side before React mounts — but a
  // background refetch (staleTime 60s) discovering a session expired while
  // already inside the SPA could otherwise render this shell with no user.
  if (!session.authenticated) {
    return <Navigate to="/login" replace />
  }

  const initials = (session.user?.username ?? '?').slice(0, 2).toUpperCase()

  return (
    <div style={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
      <header className="hdr">
        <Link to="/" className="lg">nagipath</Link>
        {session.demoMode && <span className="bg d">DEMO</span>}
        {crumb && (
          <span className="crumb">
            {crumb.section && <>{crumb.section} /</>} <b>{crumb.label}</b>
          </span>
        )}
        <div style={{ flex: 1 }} />
        <GlobalSearch />
        <button className="av" onClick={() => logout()} title="Sign out">{initials}</button>
      </header>

      <div className="wrap">
        <nav className="nav">
          {navGroups.map((group) => (
            <div key={group.section}>
              <div className="ngrp">{group.section}</div>
              {group.items.map((item) => {
                const count = item.countKey ? session.navCounts?.[item.countKey] : undefined
                const on = pathname === item.href
                const Icon = navIcons[item.icon]
                return (
                  <Link key={item.href} to={item.href} className={`ni${on ? ' on' : ''}`}>
                    <span className="ic"><Icon /></span>
                    {item.label}
                    {count !== undefined && <span className={`ct${item.warnOnCount && count > 0 ? ' warn' : ''}`}>{count}</span>}
                  </Link>
                )
              })}
            </div>
          ))}
          <div style={{ flex: 1 }} />
          <Link to={settingsHref} className={`ni${pathname.startsWith(settingsHref) ? ' on' : ''}`}>
            <span className="ic"><SettingsIcon /></span>Settings
          </Link>
        </nav>
        <main className="main">{children}</main>
      </div>
    </div>
  )
}
