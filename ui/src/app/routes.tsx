import { Route, Routes } from 'react-router-dom'
import { AppShell } from '../components/layout/AppShell'
import { Dashboard } from '../pages/dashboard/Dashboard'
import { ClustersPage } from '../pages/clusters/ClustersPage'
import { SitesListPage } from '../pages/sites/SitesListPage'
import { SiteDetailPage } from '../pages/sites/SiteDetailPage'
import { NodesListPage } from '../pages/nodes/NodesListPage'
import { NodeDetailPage } from '../pages/nodes/NodeDetailPage'
import { DriftPage } from '../pages/drift/DriftPage'
import { DriftReviewPage } from '../pages/drift/DriftReviewPage'
import { CertificatesListPage } from '../pages/certificates/CertificatesListPage'
import { CertificateDetailPage } from '../pages/certificates/CertificateDetailPage'
import { SnapshotsListPage } from '../pages/snapshots/SnapshotsListPage'
import { SnapshotFilePage } from '../pages/snapshots/SnapshotFilePage'

// Every other nav link in AppShell still points at an internal/web
// server-rendered page — a plain <a href>, a normal full-page navigation
// away from the SPA — until its own phase ports it (docs/adr/0017).
export function AppRoutes() {
  return (
    <Routes>
      <Route path="/" element={<AppShell><Dashboard /></AppShell>} />
      <Route path="/clusters" element={<AppShell><ClustersPage /></AppShell>} />
      <Route path="/sites" element={<AppShell><SitesListPage /></AppShell>} />
      <Route path="/sites/:name" element={<AppShell><SiteDetailPage /></AppShell>} />
      <Route path="/nodes" element={<AppShell><NodesListPage /></AppShell>} />
      <Route path="/nodes/:id" element={<AppShell><NodeDetailPage /></AppShell>} />
      <Route path="/drift" element={<AppShell><DriftPage /></AppShell>} />
      <Route path="/drift/review/:instanceID" element={<AppShell><DriftReviewPage /></AppShell>} />
      <Route path="/certificates" element={<AppShell><CertificatesListPage /></AppShell>} />
      <Route path="/certificates/:id" element={<AppShell><CertificateDetailPage /></AppShell>} />
      <Route path="/snapshots" element={<AppShell><SnapshotsListPage /></AppShell>} />
      <Route path="/snapshots/:id/file/:fileID" element={<AppShell><SnapshotFilePage /></AppShell>} />
    </Routes>
  )
}
