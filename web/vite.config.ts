import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// The hub serves the built UI from web/dist (embedded). In development, run
// the hub with --demo on :8484 and `npm run dev`; API and SSE calls are proxied.
export default defineConfig({
  plugins: [react(), tailwindcss()],
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
