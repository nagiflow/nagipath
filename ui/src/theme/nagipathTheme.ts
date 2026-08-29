import type { EuiThemeModifications } from '@elastic/eui'

// Ported from internal/web/static/app.src.css's @theme block — the same
// design tokens the (now-deleted) server-rendered UI used, so the SPA reads
// as the same product rather than default EUI/Kibana chrome.
export const nagipathTheme: EuiThemeModifications = {
  // The old UI never wired up prefers-color-scheme (app.src.css: "color-scheme:
  // light" only) — only LIGHT is overridden here for the same reason.
  colors: {
    LIGHT: {
      primary: '#0077cc', // --color-accent
      success: '#00726b', // --color-ok
      warning: '#8a5300', // --color-warn
      danger: '#a1231c', // --color-err
      text: '#343741', // --color-body
      title: '#1d1e24', // --color-ink
      body: '#fafbfd', // --color-page
      emptyShade: '#ffffff', // --color-panel
      lightestShade: '#eef1f7', // --color-sunken
      borderBaseSubdued: '#d3dae6', // --color-line
    },
  },
  border: {
    radius: {
      medium: '6px', // --radius-panel
      small: '4px', // --radius-ctl
    },
  },
  font: {
    family: 'Inter, -apple-system, "Segoe UI Variable", "Segoe UI", Roboto, sans-serif',
  },
}
