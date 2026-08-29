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
import { SettingsIndexRedirect } from '../pages/settings/SettingsIndexRedirect'
import { CredentialsPage } from '../pages/settings/CredentialsPage'
import { HostKeysPage } from '../pages/settings/HostKeysPage'
import { MasterKeyPage } from '../pages/settings/MasterKeyPage'
import { CollectionDefaultsPage } from '../pages/settings/CollectionDefaultsPage'
import { RetentionPage } from '../pages/settings/RetentionPage'
import { UsersPage } from '../pages/settings/UsersPage'
import { ApiKeysPage } from '../pages/settings/ApiKeysPage'
import { AuditPage } from '../pages/settings/AuditPage'
import { LicensePage } from '../pages/settings/LicensePage'
import { SystemPage } from '../pages/settings/SystemPage'
import { CollectionsPage } from '../pages/settings/CollectionsPage'
import { RulesPage } from '../pages/rules/RulesPage'
import { SearchPage } from '../pages/search/SearchPage'
import { TracePage } from '../pages/trace/TracePage'
import { ProbeHistoryPage } from '../pages/trace/ProbeHistoryPage'
import { ProbeDetailPage } from '../pages/trace/ProbeDetailPage'

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
      <Route path="/collections" element={<AppShell><CollectionsPage /></AppShell>} />
      <Route path="/settings" element={<AppShell><SettingsIndexRedirect /></AppShell>} />
      <Route path="/settings/credentials" element={<AppShell><CredentialsPage /></AppShell>} />
      <Route path="/settings/hostkeys" element={<AppShell><HostKeysPage /></AppShell>} />
      <Route path="/settings/masterkey" element={<AppShell><MasterKeyPage /></AppShell>} />
      <Route path="/settings/collection-defaults" element={<AppShell><CollectionDefaultsPage /></AppShell>} />
      <Route path="/settings/retention" element={<AppShell><RetentionPage /></AppShell>} />
      <Route path="/settings/users" element={<AppShell><UsersPage /></AppShell>} />
      <Route path="/settings/api-keys" element={<AppShell><ApiKeysPage /></AppShell>} />
      <Route path="/settings/audit" element={<AppShell><AuditPage /></AppShell>} />
      <Route path="/settings/license" element={<AppShell><LicensePage /></AppShell>} />
      <Route path="/settings/system" element={<AppShell><SystemPage /></AppShell>} />
      <Route path="/rules" element={<AppShell><RulesPage /></AppShell>} />
      <Route path="/search" element={<AppShell><SearchPage /></AppShell>} />
      <Route path="/trace" element={<AppShell><TracePage /></AppShell>} />
      <Route path="/trace/probe" element={<AppShell><ProbeDetailPage /></AppShell>} />
      <Route path="/trace/history" element={<AppShell><ProbeHistoryPage /></AppShell>} />
    </Routes>
  )
}
