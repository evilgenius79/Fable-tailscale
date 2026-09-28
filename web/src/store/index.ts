import { create } from 'zustand'
import { createJSONStorage, persist } from 'zustand/middleware'
import type { HubInfo } from '@/api/types'
import { applyTheme, resolveTheme, type ResolvedTheme, type ThemePreference } from '@/lib/theme'
import { DEFAULT_RANGE, isRangeKey, type RangeKey } from '@/lib/time'

/** Live-stream connection state as shown in the top bar. */
export type LiveState = 'connecting' | 'connected' | 'reconnecting' | 'polling'

export interface LiveStatus {
  state: LiveState
  /** When the current state began (ms epoch). */
  since: number
  /** Last `tick` received (ms epoch) or null. */
  lastTick: number | null
  /** Number of reconnect attempts since the last successful open. */
  attempts: number
}

export interface UIState {
  // theme
  theme: ThemePreference
  resolvedTheme: ResolvedTheme
  setTheme: (t: ThemePreference) => void
  /** Called by the system-theme watcher; only matters when theme === 'system'. */
  setResolvedTheme: (t: ResolvedTheme) => void

  // navigation chrome
  sidebarCollapsed: boolean
  setSidebarCollapsed: (v: boolean) => void
  toggleSidebar: () => void
  mobileNavOpen: boolean
  setMobileNavOpen: (v: boolean) => void
  paletteOpen: boolean
  setPaletteOpen: (v: boolean) => void

  // live data
  live: LiveStatus
  setLive: (patch: Partial<LiveStatus>) => void
  hub: HubInfo | null
  setHub: (h: HubInfo | null) => void

  // global time range for charts
  range: RangeKey
  setRange: (r: RangeKey) => void
}

const STORAGE_KEY = 'tailwatch.ui'

/** localStorage guarded against private-mode/blocked-storage exceptions. */
function safeLocalStorage(): Storage {
  const memory = new Map<string, string>()
  const fallback: Storage = {
    get length() {
      return memory.size
    },
    clear: () => memory.clear(),
    getItem: (k) => memory.get(k) ?? null,
    key: (i) => Array.from(memory.keys())[i] ?? null,
    removeItem: (k) => {
      memory.delete(k)
    },
    setItem: (k, v) => {
      memory.set(k, v)
    },
  }
  try {
    const ls = window.localStorage
    const probe = '__tailwatch_probe__'
    ls.setItem(probe, '1')
    ls.removeItem(probe)
    return {
      get length() {
        return ls.length
      },
      clear: () => ls.clear(),
      getItem: (k) => {
        try {
          return ls.getItem(k)
        } catch {
          return null
        }
      },
      key: (i) => ls.key(i),
      removeItem: (k) => {
        try {
          ls.removeItem(k)
        } catch {
          /* ignore */
        }
      },
      setItem: (k, v) => {
        try {
          ls.setItem(k, v)
        } catch {
          /* quota / blocked */
        }
      },
    }
  } catch {
    return fallback
  }
}

export const useUIStore = create<UIState>()(
  persist(
    (set, get) => ({
      theme: 'system',
      resolvedTheme: resolveTheme('system'),
      setTheme: (theme) => {
        const resolvedTheme = resolveTheme(theme)
        applyTheme(resolvedTheme)
        set({ theme, resolvedTheme })
      },
      setResolvedTheme: (t) => {
        if (get().theme !== 'system') return
        applyTheme(t)
        set({ resolvedTheme: t })
      },

      sidebarCollapsed: false,
      setSidebarCollapsed: (v) => set({ sidebarCollapsed: v }),
      toggleSidebar: () => set((s) => ({ sidebarCollapsed: !s.sidebarCollapsed })),
      mobileNavOpen: false,
      setMobileNavOpen: (v) => set({ mobileNavOpen: v }),
      paletteOpen: false,
      setPaletteOpen: (v) => set({ paletteOpen: v }),

      live: { state: 'connecting', since: Date.now(), lastTick: null, attempts: 0 },
      setLive: (patch) =>
        set((s) => {
          const next = { ...s.live, ...patch }
          if (patch.state && patch.state !== s.live.state && patch.since === undefined) next.since = Date.now()
          return { live: next }
        }),
      hub: null,
      setHub: (hub) => set({ hub }),

      range: DEFAULT_RANGE,
      setRange: (range) => set({ range: isRangeKey(range) ? range : DEFAULT_RANGE }),
    }),
    {
      name: STORAGE_KEY,
      version: 1,
      storage: createJSONStorage(safeLocalStorage),
      // Only UI preferences are persisted. Never anything from the API.
      partialize: (s) => ({ theme: s.theme, sidebarCollapsed: s.sidebarCollapsed }),
      merge: (persisted, current) => {
        const p = (persisted ?? {}) as Partial<Pick<UIState, 'theme' | 'sidebarCollapsed'>>
        const theme: ThemePreference = p.theme === 'light' || p.theme === 'dark' || p.theme === 'system' ? p.theme : 'system'
        return {
          ...current,
          theme,
          resolvedTheme: resolveTheme(theme),
          sidebarCollapsed: typeof p.sidebarCollapsed === 'boolean' ? p.sidebarCollapsed : false,
        }
      },
    },
  ),
)

/** Selector helpers */
export const selectLive = (s: UIState) => s.live
export const selectIsPolling = (s: UIState) => s.live.state === 'polling'
export const selectRange = (s: UIState) => s.range
export const selectResolvedTheme = (s: UIState) => s.resolvedTheme
