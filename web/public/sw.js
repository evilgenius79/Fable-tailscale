/* Tailwatch service worker.
 *
 * Purpose: make the UI installable (PWA) and let the app shell open instantly
 * or offline. It deliberately caches only same-origin GET requests for the
 * hashed build assets (immutable, cache-first) and the HTML shell
 * (network-first with an offline fallback). API and SSE requests are never
 * touched, so live data is always fetched from the hub.
 */
const VERSION = 'tailwatch-sw-v1'
const ASSET_CACHE = `${VERSION}-assets`
const SHELL_CACHE = `${VERSION}-shell`
const SHELL_KEY = '/'

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches
      .open(SHELL_CACHE)
      .then((cache) => cache.add(new Request(SHELL_KEY, { cache: 'reload' })).catch(() => {}))
      .then(() => self.skipWaiting()),
  )
})

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) =>
        Promise.all(keys.filter((k) => k !== ASSET_CACHE && k !== SHELL_CACHE).map((k) => caches.delete(k))),
      )
      .then(() => self.clients.claim()),
  )
})

self.addEventListener('fetch', (event) => {
  const req = event.request
  if (req.method !== 'GET') return
  const url = new URL(req.url)
  if (url.origin !== self.location.origin) return
  // Never intercept the API, the event stream or health checks.
  if (url.pathname.startsWith('/api/') || url.pathname === '/healthz') return

  if (url.pathname.startsWith('/assets/')) {
    event.respondWith(cacheFirst(req))
    return
  }
  if (req.mode === 'navigate') {
    event.respondWith(networkFirstShell(req))
  }
})

async function cacheFirst(req) {
  const cache = await caches.open(ASSET_CACHE)
  const hit = await cache.match(req)
  if (hit) return hit
  const res = await fetch(req)
  if (res.ok) cache.put(req, res.clone()).catch(() => {})
  return res
}

async function networkFirstShell(req) {
  const cache = await caches.open(SHELL_CACHE)
  try {
    const res = await fetch(req)
    if (res.ok && res.headers.get('content-type')?.includes('text/html')) {
      cache.put(SHELL_KEY, res.clone()).catch(() => {})
    }
    return res
  } catch {
    const cached = await cache.match(SHELL_KEY)
    if (cached) return cached
    return new Response(
      '<!doctype html><meta charset="utf-8"><title>Tailwatch</title>' +
        '<style>body{font-family:system-ui,sans-serif;background:#0a0a0c;color:#e5e5e7;display:grid;place-items:center;height:100vh;margin:0}' +
        'p{max-width:32ch;text-align:center;line-height:1.5}</style>' +
        '<p><strong>Tailwatch is offline.</strong><br>Connect to your tailnet and reload to reach the hub.</p>',
      { status: 503, headers: { 'Content-Type': 'text/html; charset=utf-8' } },
    )
  }
}
