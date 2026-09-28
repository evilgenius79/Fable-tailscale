export type ThemePreference = 'system' | 'light' | 'dark'
export type ResolvedTheme = 'light' | 'dark'

const MEDIA = '(prefers-color-scheme: dark)'

/** What the OS currently prefers (dark when matchMedia is unavailable, to match index.html). */
export function systemTheme(): ResolvedTheme {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return 'dark'
  try {
    return window.matchMedia(MEDIA).matches ? 'dark' : 'light'
  } catch {
    return 'dark'
  }
}

/** Resolve a preference to a concrete theme. */
export function resolveTheme(pref: ThemePreference): ResolvedTheme {
  return pref === 'system' ? systemTheme() : pref
}

/** Apply the `dark` class on <html> and update the meta color-scheme. Idempotent. */
export function applyTheme(resolved: ResolvedTheme): void {
  if (typeof document === 'undefined') return
  const root = document.documentElement
  root.classList.toggle('dark', resolved === 'dark')
  root.style.colorScheme = resolved
  const meta = document.querySelector<HTMLMetaElement>('meta[name="theme-color"]')
  const color = resolved === 'dark' ? '#0a0a0c' : '#f6f6f8'
  if (meta) meta.content = color
  else {
    const m = document.createElement('meta')
    m.name = 'theme-color'
    m.content = color
    document.head.appendChild(m)
  }
}

/** Subscribe to OS theme changes. Returns an unsubscribe function. */
export function watchSystemTheme(cb: (t: ResolvedTheme) => void): () => void {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return () => {}
  let mq: MediaQueryList
  try {
    mq = window.matchMedia(MEDIA)
  } catch {
    return () => {}
  }
  const handler = (e: MediaQueryListEvent) => cb(e.matches ? 'dark' : 'light')
  mq.addEventListener('change', handler)
  return () => mq.removeEventListener('change', handler)
}

/** Cycle order for a single toggle button: system → light → dark → system. */
export function nextTheme(pref: ThemePreference): ThemePreference {
  return pref === 'system' ? 'light' : pref === 'light' ? 'dark' : 'system'
}
