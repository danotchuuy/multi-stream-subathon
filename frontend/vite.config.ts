import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    // Listen on all interfaces (not just localhost) so the dashboard and
    // overlay can be reached from other devices on the network, e.g. a
    // second machine running OBS or a phone. Dev-only convenience — don't
    // expose this to an untrusted network.
    host: true,
    allowedHosts: true,
    proxy: {
      '/api': 'http://localhost:8090',
      // OAuth login/link/logout and /api/me. Proxying this too (rather
      // than pointing the browser straight at the API port) keeps the
      // whole app same-origin from the browser's perspective, so the
      // session cookie the OAuth callback sets is scoped to this origin
      // and just works with the SPA's fetches.
      '/auth': 'http://localhost:8090',
      '/ws': {
        target: 'ws://localhost:8090',
        ws: true,
      },
    },
  },
})
