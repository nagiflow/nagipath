import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
// Self-hosted (not Google Fonts CDN) — nagipath is a network/config-audit
// tool that plausibly runs in air-gapped or otherwise internet-restricted
// deployments; the UI's legibility shouldn't depend on an external CDN being
// reachable. Weights match design/'s own Google-Fonts request
// (Inter 400/500/600/700, Roboto Mono 400/500).
import '@fontsource/inter/400.css'
import '@fontsource/inter/500.css'
import '@fontsource/inter/600.css'
import '@fontsource/inter/700.css'
import '@fontsource/roboto-mono/400.css'
import '@fontsource/roboto-mono/500.css'
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
