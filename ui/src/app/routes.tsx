import { Route, Routes } from 'react-router-dom'
import { AppShell } from '../components/layout/AppShell'
import { Dashboard } from '../pages/dashboard/Dashboard'
import { ClustersPage } from '../pages/clusters/ClustersPage'

// Every other nav link in AppShell still points at an internal/web
// server-rendered page — a plain <a href>, a normal full-page navigation
// away from the SPA — until its own phase ports it (docs/adr/0017).
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
      <Route
        path="/clusters"
        element={
          <AppShell>
            <ClustersPage />
          </AppShell>
        }
      />
    </Routes>
  )
}
