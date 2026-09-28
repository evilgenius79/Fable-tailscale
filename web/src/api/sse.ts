// Live stream: EventSource('/api/v1/stream') → query cache + store.
//
// * `tick`  → overview + device list (+ per-device detail caches)
// * `event` → prepended to cached event lists; warning/critical raise a toast
// * `alert` → alerts invalidated; toast for open/resolve/ack
// * `hello` → identity + hub
//
// The browser reconnects EventSource automatically (readyState CONNECTING).
// If the server closes the stream (readyState CLOSED) we reconnect ourselves
// with exponential backoff. After 30s without a connection the store flips to
// `polling`, which makes the live hooks refetch every 15s.

import { useEffect } from 'react'
import { useQueryClient, type QueryClient } from '@tanstack/react-query'
import { API_BASE } from './client'
import { queryKeys, type EventsParams } from './queryKeys'
import type { Alert, Device, DeviceDetail, Event, Overview, SSEHello, SSETick } from './types'
import { useUIStore } from '../store'
import { toast } from '../components/ui/Toast'

export const STREAM_URL = `${API_BASE}/stream`
export const POLLING_FALLBACK_AFTER_MS = 30_000

function parse<T>(ev: MessageEvent): T | null {
  try {
    return JSON.parse(String(ev.data)) as T
  } catch {
    return null
  }
}

/** Does a live event belong in the cached list for `params`? `type` may be a comma-separated list. Exported for tests. */
export function eventMatches(params: EventsParams, e: Event): boolean {
  if (params.type && !params.type.split(',').includes(e.type)) return false
  if (params.device && params.device !== e.deviceId) return false
  return true
}

/** Apply a tick to the cache. Exported for tests. */
export function applyTick(qc: QueryClient, tick: SSETick): void {
  qc.setQueryData<Overview>(queryKeys.overview, tick.overview)
  const sorted = [...tick.devices].sort((a, b) => a.name.localeCompare(b.name))
  qc.setQueryData<Device[]>(queryKeys.devices, sorted)
  for (const d of tick.devices) {
    qc.setQueryData<DeviceDetail>(queryKeys.device(d.id), (old) => (old ? { ...old, device: d } : old))
  }
}

/** Prepend an event to every cached event list it belongs in. Exported for tests. */
export function applyEvent(qc: QueryClient, e: Event): void {
  for (const q of qc.getQueryCache().findAll({ queryKey: queryKeys.eventsAll })) {
    const key = q.queryKey as readonly unknown[]
    if (key[1] !== 'list') continue
    const params = (key[2] ?? {}) as EventsParams
    if (!eventMatches(params, e)) continue
    qc.setQueryData<Event[]>(q.queryKey, (old) => {
      if (!old) return old
      if (old.some((x) => x.id === e.id)) return old
      const limit = params.limit ?? 100
      return [e, ...old].slice(0, Math.max(limit, 1))
    })
  }
  if (e.deviceId) {
    for (const q of qc.getQueryCache().findAll({ queryKey: queryKeys.deviceEventsAll(e.deviceId) })) {
      qc.setQueryData<Event[]>(q.queryKey, (old) => (old && !old.some((x) => x.id === e.id) ? [e, ...old] : old))
    }
    qc.setQueryData<DeviceDetail>(queryKeys.device(e.deviceId), (old) =>
      old && !old.recentEvents.some((x) => x.id === e.id) ? { ...old, recentEvents: [e, ...old.recentEvents].slice(0, 20) } : old,
    )
  }
}

/** Apply an alert change to the cache. Exported for tests. */
export function applyAlert(qc: QueryClient, a: Alert): void {
  qc.setQueriesData<Alert[]>({ queryKey: queryKeys.alertsAll }, (old) => {
    if (!Array.isArray(old)) return old
    const idx = old.findIndex((x) => x.id === a.id)
    return idx >= 0 ? old.map((x) => (x.id === a.id ? a : x)) : old
  })
  void qc.invalidateQueries({ queryKey: queryKeys.alertsAll })
  if (a.deviceId) {
    qc.setQueryData<DeviceDetail>(queryKeys.device(a.deviceId), (old) => {
      if (!old) return old
      const without = old.openAlerts.filter((x) => x.id !== a.id)
      return { ...old, openAlerts: a.state === 'open' ? [a, ...without] : without }
    })
  }
}

function toastForEvent(e: Event) {
  if (e.severity !== 'warning' && e.severity !== 'critical') return
  // Alert lifecycle events get their toast from the `alert` stream.
  if (e.type.startsWith('alert.')) return
  toast({ id: `event-${e.id}`, tone: e.severity === 'critical' ? 'error' : 'warning', title: e.title, description: e.message })
}

// Alerts this client acknowledged itself (id → deadline). The ack mutation
// raises its own confirmation toast, so the stream's "Acknowledged: …" for the
// same alert is dropped instead of stacking a second toast for one action.
const localAcks = new Map<number, number>()
const LOCAL_ACK_TTL_MS = 15_000

/** Record that this client is acknowledging `id` (called by `useAckAlert` before the request). */
export function markLocalAck(id: number): void {
  localAcks.set(id, Date.now() + LOCAL_ACK_TTL_MS)
}

function consumeLocalAck(id: number): boolean {
  const until = localAcks.get(id)
  if (until === undefined) return false
  localAcks.delete(id)
  return until > Date.now()
}

/** Toast for an alert from the stream. Ids are `alert-<id>`, so a later change to the same alert replaces the toast. Exported for tests. */
export function toastForAlert(a: Alert): void {
  if (a.state === 'resolved') {
    toast({ id: `alert-${a.id}`, tone: 'success', title: `Resolved: ${a.title}`, description: a.deviceName })
  } else if (a.ackedAt) {
    if (consumeLocalAck(a.id)) return
    toast({ id: `alert-${a.id}`, tone: 'info', title: `Acknowledged: ${a.title}`, description: a.ackedBy ? `by ${a.ackedBy}` : undefined })
  } else {
    toast({ id: `alert-${a.id}`, tone: a.severity === 'critical' ? 'error' : a.severity === 'warning' ? 'warning' : 'info', title: a.title, description: a.message })
  }
}

/**
 * Mount once inside the QueryClientProvider (App does this). Keeps the query
 * cache warm from the SSE stream and reports connection state to the store.
 */
export function useLiveStream(enabled = true): void {
  const qc = useQueryClient()
  useEffect(() => {
    if (!enabled) return
    const { setLive, setHub } = useUIStore.getState()
    let es: EventSource | null = null
    let closed = false
    let everConnected = false
    let attempts = 0
    let pollTimer: ReturnType<typeof setTimeout> | null = null
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null

    const armPollingFallback = () => {
      if (pollTimer) return
      pollTimer = setTimeout(() => {
        pollTimer = null
        if (!closed) setLive({ state: 'polling' })
      }, POLLING_FALLBACK_AFTER_MS)
    }
    const disarmPollingFallback = () => {
      if (pollTimer) clearTimeout(pollTimer)
      pollTimer = null
    }

    const connect = () => {
      if (closed) return
      const ES = globalThis.EventSource
      if (typeof ES !== 'function') {
        setLive({ state: 'polling' })
        return
      }
      try {
        es = new ES(STREAM_URL)
      } catch {
        setLive({ state: 'polling' })
        return
      }
      setLive({ state: everConnected ? 'reconnecting' : 'connecting', attempts })
      armPollingFallback()

      es.onopen = () => {
        attempts = 0
        disarmPollingFallback()
        const wasDown = everConnected
        everConnected = true
        setLive({ state: 'connected', attempts: 0 })
        if (wasDown) {
          // No Last-Event-ID replay: refetch everything we may have missed.
          void qc.invalidateQueries()
          toast({ id: 'live-stream', tone: 'success', title: 'Live updates restored', duration: 3000 })
        }
      }
      es.addEventListener('hello', (ev) => {
        const hello = parse<SSEHello>(ev as MessageEvent)
        if (!hello) return
        if (hello.identity) qc.setQueryData(queryKeys.me, hello.identity)
        if (hello.hub) setHub(hello.hub)
      })
      es.addEventListener('tick', (ev) => {
        const tick = parse<SSETick>(ev as MessageEvent)
        if (!tick || !tick.overview || !Array.isArray(tick.devices)) return
        applyTick(qc, tick)
        setHub(tick.overview.hub)
        setLive({ lastTick: Date.now() })
      })
      es.addEventListener('event', (ev) => {
        const e = parse<Event>(ev as MessageEvent)
        if (!e || typeof e.id !== 'number') return
        applyEvent(qc, e)
        toastForEvent(e)
      })
      es.addEventListener('alert', (ev) => {
        const a = parse<Alert>(ev as MessageEvent)
        if (!a || typeof a.id !== 'number') return
        applyAlert(qc, a)
        toastForAlert(a)
      })
      es.onerror = () => {
        if (closed || !es) return
        attempts += 1
        armPollingFallback()
        const current = useUIStore.getState().live.state
        if (current !== 'polling') setLive({ state: 'reconnecting', attempts })
        if (es.readyState === 2 /* CLOSED */) {
          // Server closed us; EventSource will not retry on its own.
          es.close()
          es = null
          const backoff = Math.min(30_000, 1000 * 2 ** Math.min(attempts, 5))
          reconnectTimer = setTimeout(connect, backoff)
        }
      }
    }

    connect()

    // If the tab was hidden for a long time the browser may have throttled us;
    // refetch once it becomes visible again.
    const onVisible = () => {
      if (document.visibilityState === 'visible' && everConnected) void qc.invalidateQueries({ queryKey: queryKeys.overview })
    }
    document.addEventListener('visibilitychange', onVisible)

    return () => {
      closed = true
      document.removeEventListener('visibilitychange', onVisible)
      disarmPollingFallback()
      if (reconnectTimer) clearTimeout(reconnectTimer)
      es?.close()
      es = null
    }
  }, [qc, enabled])
}
