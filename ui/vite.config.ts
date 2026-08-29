import react from '@vitejs/plugin-react'
import { defineConfig } from 'vitest/config'

// build.outDir feeds internal/web/embed.go's `//go:embed ui/dist` — the
// build output is committed (ADR-0016's precedent for app.css, extended to
// the whole UI: `go build`/`go install`/an air-gapped clone need no Node).
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: '../internal/web/ui/dist',
    emptyOutDir: true,
  },
  server: {
    // `bun run dev` talks to a `nagipath server` running on :8080 for
    // everything the SPA doesn't render itself.
    proxy: {
      '/api': 'http://127.0.0.1:8080',
    },
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
  },
})
