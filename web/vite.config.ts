import { createHash } from 'node:crypto'
import { existsSync, readFileSync, writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { defineConfig, type Plugin } from 'vitest/config'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

/**
 * Stamp public/sw.js with a per-build version. The service worker caches the
 * hashed /assets/* bundles cache-first under a cache named after VERSION and
 * only deletes caches with *other* names on activate, so a fixed VERSION would
 * let every release's chunks pile up on each client forever. A version derived
 * from the emitted file names gives each release its own cache (and refreshes
 * the shell cache); the previous one is dropped on activate.
 */
function swVersion(): Plugin {
  let hash = ''
  let outDir = ''
  return {
    name: 'tailwatch-sw-version',
    apply: 'build',
    configResolved(c) {
      outDir = resolve(c.root, c.build.outDir)
    },
    generateBundle(_, bundle) {
      hash = createHash('sha1').update(Object.keys(bundle).sort().join('\n')).digest('hex').slice(0, 12)
    },
    closeBundle() {
      const file = resolve(outDir, 'sw.js')
      if (!hash || !existsSync(file)) return
      const src = readFileSync(file, 'utf8')
      const marker = "const VERSION = 'tailwatch-sw-v1'"
      if (!src.includes(marker)) throw new Error('sw.js: VERSION marker not found; keep it in sync with vite.config.ts')
      writeFileSync(file, src.replace(marker, `const VERSION = 'tailwatch-sw-${hash}'`))
    },
  }
}

// The hub serves the built UI from web/dist (embedded). In development, run
// the hub with --demo on :8484 and `npm run dev`; API and SSE calls are proxied.
export default defineConfig({
  plugins: [react(), tailwindcss(), swVersion()],
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    sourcemap: false,
    target: 'es2022',
  },
  server: {
    port: 5173,
    proxy: {
      '/api': { target: 'http://127.0.0.1:8484', changeOrigin: false },
      '/healthz': { target: 'http://127.0.0.1:8484', changeOrigin: false },
    },
  },
  test: {
    environment: 'jsdom',
    globals: false,
    include: ['src/**/*.test.{ts,tsx}'],
  },
})
