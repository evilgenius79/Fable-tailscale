// View state for the Devices page: filters + sort live in the URL query string
// (shareable, back-button friendly); density and column choices are UI
// preferences kept in localStorage (guarded, never anything from the API).

import { useCallback, useMemo, useRef, useState, useSyncExternalStore } from 'react'
import { useSearchParams } from 'react-router-dom'
import type { Device } from '../../api/types'
import type { SortState } from '../ui/Table'
import { DEFAULT_VIEW_PREFS, sanitizeViewPrefs, type DevicesViewPrefs } from './columns'
import { SORT_KEYS, parseFilters, parseSort, serializeView, type DeviceFilters } from './deviceFilters'

export interface DevicesView {
  filters: DeviceFilters
  sort: SortState
  setFilters: (patch: Partial<DeviceFilters>) => void
  replaceFilters: (next: DeviceFilters) => void
  setSort: (next: SortState) => void
  toggleSort: (key: string) => void
}

export function useDevicesView(): DevicesView {
  const [sp, setSp] = useSearchParams()
  const filters = useMemo(() => parseFilters(sp), [sp])
  const sort = useMemo(() => parseSort(sp, SORT_KEYS), [sp])

  const setFilters = useCallback(
    (patch: Partial<DeviceFilters>) => {
      setSp((prev) => serializeView({ ...parseFilters(prev), ...patch }, parseSort(prev, SORT_KEYS)), { replace: true })
    },
    [setSp],
  )
  const replaceFilters = useCallback(
    (next: DeviceFilters) => {
      setSp((prev) => serializeView(next, parseSort(prev, SORT_KEYS)), { replace: true })
    },
    [setSp],
  )
  const setSort = useCallback(
    (next: SortState) => {
      setSp((prev) => serializeView(parseFilters(prev), next), { replace: true })
    },
    [setSp],
  )
  const toggleSort = useCallback(
    (key: string) => {
      setSp(
        (prev) => {
          const cur = parseSort(prev, SORT_KEYS)
          const next: SortState = cur.key === key ? { key, dir: cur.dir === 'asc' ? 'desc' : 'asc' } : { key, dir: 'asc' }
          return serializeView(parseFilters(prev), next)
        },
        { replace: true },
      )
    },
    [setSp],
  )

  return { filters, sort, setFilters, replaceFilters, setSort, toggleSort }
}

// ---------------------------------------------------------------------------
// Persisted UI preferences (density, columns) — non-sensitive, guarded.
// ---------------------------------------------------------------------------

const PREFS_KEY = 'tailwatch.devices'

function readPrefs(): DevicesViewPrefs {
  try {
    const raw = window.localStorage.getItem(PREFS_KEY)
    return raw ? sanitizeViewPrefs(JSON.parse(raw)) : DEFAULT_VIEW_PREFS
  } catch {
    return DEFAULT_VIEW_PREFS
  }
}

function writePrefs(p: DevicesViewPrefs) {
  try {
    window.localStorage.setItem(PREFS_KEY, JSON.stringify(p))
  } catch {
    /* private mode / quota — keep in memory only */
  }
}

export function useViewPrefs(): [DevicesViewPrefs, (next: DevicesViewPrefs) => void] {
  const [prefs, setPrefs] = useState<DevicesViewPrefs>(readPrefs)
  const set = useCallback((next: DevicesViewPrefs) => {
    setPrefs(next)
    writePrefs(next)
  }, [])
  return [prefs, set]
}

// ---------------------------------------------------------------------------
// Media query (table vs cards)
// ---------------------------------------------------------------------------

export function useMediaQuery(query: string): boolean {
  const subscribe = useCallback(
    (cb: () => void) => {
      if (typeof window === 'undefined' || !window.matchMedia) return () => {}
      const m = window.matchMedia(query)
      m.addEventListener('change', cb)
      return () => m.removeEventListener('change', cb)
    },
    [query],
  )
  const get = useCallback(() => (typeof window !== 'undefined' && !!window.matchMedia ? window.matchMedia(query).matches : false), [query])
  return useSyncExternalStore(subscribe, get, () => false)
}

// ---------------------------------------------------------------------------
// Live rate history — a per-device ring buffer of rx+tx fed by SSE ticks.
// ---------------------------------------------------------------------------

interface HistoryEntry {
  last: string
  values: number[]
}

/** Recent rx+tx totals per device id (grows by one point per distinct `updatedAt`). */
export function useRateHistory(devices: ReadonlyArray<Device> | undefined, size = 24): ReadonlyMap<string, ReadonlyArray<number>> {
  const store = useRef<Map<string, HistoryEntry>>(new Map())
  return useMemo(() => {
    const m = store.current
    const seen = new Set<string>()
    for (const d of devices ?? []) {
      seen.add(d.id)
      const entry = m.get(d.id) ?? { last: '', values: [] }
      if (entry.last !== d.updatedAt) {
        entry.last = d.updatedAt
        entry.values = [...entry.values, d.online ? d.connectivity.rxRate + d.connectivity.txRate : 0].slice(-size)
        m.set(d.id, entry)
      }
    }
    for (const id of Array.from(m.keys())) if (!seen.has(id)) m.delete(id)
    const out = new Map<string, ReadonlyArray<number>>()
    for (const [id, e] of m) out.set(id, e.values)
    return out
  }, [devices, size])
}
