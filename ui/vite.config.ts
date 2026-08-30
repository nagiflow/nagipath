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
    // 0.0.0.0 so the dev server is reachable from outside its container in
    // `make lab` (testlab/docker-compose.yml's ui service); harmless for a
    // plain host-side `bun run dev` too.
    host: true,
    // `bun run dev` talks to a `nagipath server` for everything the SPA
    // doesn't render itself — :8080 on the host by default, or the lab's
    // `nagipath` container over the compose network (VITE_API_PROXY_TARGET).
    proxy: {
      '/api': process.env.VITE_API_PROXY_TARGET || 'http://127.0.0.1:8080',
    },
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
  },
})
