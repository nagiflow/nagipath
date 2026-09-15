import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
// Self-hosted (not Google Fonts CDN) — nagipath is a network/config-audit
// tool that plausibly runs in air-gapped or otherwise internet-restricted
// deployments; the UI's legibility shouldn't depend on an external CDN being
// reachable.
import './theme/fonts.css'
import './index.css'
import { AppProviders } from './app/providers'
import { AppRoutes } from './app/routes'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <AppProviders>
      <AppRoutes />
    </AppProviders>
  </StrictMode>,
)
