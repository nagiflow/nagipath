import { Route, Routes } from 'react-router-dom'
import { AppShell } from '../components/layout/AppShell'
import { Dashboard } from '../pages/dashboard/Dashboard'

// One route so far (docs/adr/0017, Phase 1). Every other nav link in
// AppShell still points at an internal/web server-rendered page — a plain
// <a href>, which is a normal full-page navigation away from the SPA.
export function AppRoutes() {
  return (
    <Routes>
      <Route
        path="/"
        element={
          <AppShell>
            <Dashboard />
          </AppShell>
        }
      />
    </Routes>
  )
}
