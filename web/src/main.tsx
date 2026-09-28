import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import App from './App'
import { applyTheme, watchSystemTheme } from './lib/theme'
import { useUIStore } from './store'

async function boot() {
  // Mock API for `VITE_MOCK=1 vite`. The condition is statically false in
  // production builds, so the dynamic import (and mockData) is tree-shaken.
  if (import.meta.env.DEV && import.meta.env.VITE_MOCK === '1') {
    const { installMock } = await import('./api/mock')
    installMock()
  }

  // Theme: persisted preference, falling back to the OS setting.
  applyTheme(useUIStore.getState().resolvedTheme)
  watchSystemTheme((t) => useUIStore.getState().setResolvedTheme(t))

  // Installable app: register the service worker in production builds only.
  // It caches the shell and hashed assets, never API responses (see public/sw.js).
  if (import.meta.env.PROD && 'serviceWorker' in navigator) {
    window.addEventListener('load', () => {
      navigator.serviceWorker.register('/sw.js', { scope: '/' }).catch(() => {
        /* the app works without it */
      })
    })
  }

  createRoot(document.getElementById('root')!).render(
    <StrictMode>
      <App />
    </StrictMode>,
  )
}

void boot()
