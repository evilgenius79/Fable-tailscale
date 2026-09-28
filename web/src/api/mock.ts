// Development-only mock of the hub API. Installed by main.tsx when
// `import.meta.env.DEV && import.meta.env.VITE_MOCK === '1'`; that condition is
// statically false in production builds so this module is never bundled.

import type { Alert, AlertRule, Device, Event, PingResult, SSEHello, SSETick } from './types'
import { isRangeKey, type RangeKey } from '../lib/time'
import {
  advance,
  buildSeries,
  buildSettings,
  buildTopology,
  buildUptime,
  createMockState,
  deviceDetail,
  findDevice,
  findProfile,
  overview,
  refreshDevices,
  type MockState,
} from './mockData'

type Json = unknown

const API = '/api/v1'
const LATENCY_MIN = 60
const LATENCY_MAX = 220
const TICK_MS = 5000

let installed = false
let state: MockState
const sources = new Set<MockEventSource>()
let ticker: ReturnType<typeof setInterval> | null = null

function delay(): Promise<void> {
  const ms = LATENCY_MIN + Math.random() * (LATENCY_MAX - LATENCY_MIN)
  return new Promise((r) => setTimeout(r, ms))
}

function json(body: Json, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json; charset=utf-8', 'cache-control': 'no-store' } })
}

function err(code: string, message: string, status: number): Response {
  return json({ error: { code, message } }, status)
}

function headerValue(init: RequestInit | undefined, input: RequestInfo | URL, name: string): string | null {
  const h = init?.headers
  if (h instanceof Headers) return h.get(name)
  if (Array.isArray(h)) {
    const hit = h.find(([k]) => k.toLowerCase() === name.toLowerCase())
    return hit ? hit[1] : null
  }
  if (h && typeof h === 'object') {
    for (const [k, v] of Object.entries(h)) if (k.toLowerCase() === name.toLowerCase()) return v
  }
  if (input instanceof Request) return input.headers.get(name)
  return null
}

async function readBody<T>(init: RequestInit | undefined, input: RequestInfo | URL): Promise<T | undefined> {
  const raw = init?.body ?? (input instanceof Request ? await input.text() : undefined)
  if (raw === undefined || raw === null) return undefined
  if (typeof raw !== 'string') return undefined
  try {
    return JSON.parse(raw) as T
  } catch {
    return undefined
  }
}

function audit(actor: string, action: string, target: string, details?: Record<string, unknown>, ok = true, error?: string) {
  state.audit.unshift({ id: state.nextAuditId++, ts: new Date().toISOString(), actor, actorNode: state.identity.nodeName, action, target, details, ok, error, remoteIp: state.identity.nodeIp })
  state.events.unshift({
    id: state.nextEventId++,
    ts: new Date().toISOString(),
    type: 'admin.action',
    severity: ok ? 'info' : 'warning',
    deviceId: action.startsWith('device.') ? target : undefined,
    deviceName: action.startsWith('device.') ? findDevice(state, target)?.name : undefined,
    title: `${action} ${findDevice(state, target)?.name ?? target}${ok ? '' : ' failed'}`,
    message: `${actor} · ${ok ? 'ok' : error}`,
    data: details,
  })
}

function broadcastEvent(e: Event) {
  for (const es of sources) es.emit('event', e)
}
function broadcastAlert(a: Alert) {
  for (const es of sources) es.emit('alert', a)
}
function broadcastTick() {
  const now = Date.now()
  const devices = refreshDevices(state, now)
  const tick: SSETick = { overview: overview(state, now), devices }
  for (const es of sources) es.emit('tick', tick)
}

function mutateDevice(id: string, patch: Partial<Device>): Device | undefined {
  const d = findDevice(state, id)
  if (!d) return undefined
  const prev = state.overrides.get(d.id) ?? {}
  state.overrides.set(d.id, { ...prev, ...patch })
  refreshDevices(state, Date.now())
  return findDevice(state, d.id)
}

async function route(method: string, path: string, params: URLSearchParams, init: RequestInit | undefined, input: RequestInfo | URL): Promise<Response> {
  const now = Date.now()
  const segs = path.split('/').filter(Boolean)
  const actor = state.identity.login

  // ---- collections --------------------------------------------------------
  if (method === 'GET' && path === '/me') return json(state.identity)
  if (method === 'GET' && path === '/overview') {
    refreshDevices(state, now)
    return json(overview(state, now))
  }
  if (method === 'GET' && path === '/settings') return json(buildSettings(now, state))
  if (method === 'GET' && path === '/devices') {
    const list = [...refreshDevices(state, now)].sort((a, b) => a.name.localeCompare(b.name))
    return json(list)
  }
  if (method === 'GET' && path === '/events') {
    const limit = Math.min(500, Number(params.get('limit') ?? 100) || 100)
    const type = params.get('type')
    const device = params.get('device')
    const since = params.get('since')
    let list = state.events
    if (type) list = list.filter((e) => e.type === type)
    if (device) {
      const p = findProfile(device)
      list = list.filter((e) => e.deviceId === (p?.id ?? device))
    }
    if (since) list = list.filter((e) => e.ts > since)
    return json(list.slice(0, limit))
  }
  if (method === 'GET' && path === '/alerts') {
    const st = params.get('state') ?? 'open'
    const device = params.get('device')
    const limit = Math.min(500, Number(params.get('limit') ?? 200) || 200)
    let list = state.alerts
    if (st === 'open' || st === 'resolved') list = list.filter((a) => a.state === st)
    if (device) {
      const p = findProfile(device)
      list = list.filter((a) => a.deviceId === (p?.id ?? device))
    }
    return json([...list].sort((a, b) => (a.openedAt < b.openedAt ? 1 : -1)).slice(0, limit))
  }
  if (method === 'GET' && path === '/alerts/rules') return json(state.rules)
  if (method === 'GET' && path === '/network/topology') {
    refreshDevices(state, now)
    return json(buildTopology(state.devices))
  }
  if (method === 'GET' && path === '/audit') {
    if (state.identity.role !== 'admin') return err('forbidden', 'admin role required', 403)
    const limit = Math.min(500, Number(params.get('limit') ?? 100) || 100)
    return json(state.audit.slice(0, limit))
  }
  if (method === 'POST' && path === '/refresh') {
    setTimeout(broadcastTick, 400)
    return new Response(null, { status: 202 })
  }
  if (method === 'POST' && path === '/alerts/test') {
    const body = await readBody<{ message?: string }>(init, input)
    audit(actor, 'alerts.test', 'webhook', { message: body?.message ?? '' })
    return json({ sent: ['webhook', 'ntfy'], errors: {} })
  }

  // ---- alerts/rules/{id} ----------------------------------------------------
  if (method === 'PUT' && segs[0] === 'alerts' && segs[1] === 'rules' && segs[2]) {
    const id = decodeURIComponent(segs[2])
    const body = await readBody<AlertRule>(init, input)
    if (!body || typeof body !== 'object') return err('bad_request', 'invalid rule body', 400)
    const idx = state.rules.findIndex((r) => r.id === id)
    if (idx < 0) return err('not_found', 'rule not found', 404)
    const cur = state.rules[idx]!
    if (typeof body.threshold !== 'number' || body.threshold < 0) return err('bad_request', 'threshold must be a non-negative number', 400)
    const next: AlertRule = { ...cur, ...body, id, type: cur.type, updatedAt: new Date().toISOString() }
    state.rules[idx] = next
    audit(actor, 'rule.save', id, { enabled: next.enabled, threshold: next.threshold, forSeconds: next.forSeconds })
    return json(next)
  }

  // ---- alerts/{id}/ack ------------------------------------------------------
  if (method === 'POST' && segs[0] === 'alerts' && segs[2] === 'ack') {
    const id = Number(segs[1])
    const a = state.alerts.find((x) => x.id === id)
    if (!a) return err('not_found', 'alert not found', 404)
    if (a.state !== 'open') return err('bad_request', 'alert is not open', 400)
    a.ackedAt = new Date().toISOString()
    a.ackedBy = actor
    a.updatedAt = a.ackedAt
    audit(actor, 'alert.ack', String(id))
    const ev: Event = { id: state.nextEventId++, ts: a.ackedAt, type: 'alert.acked', severity: 'info', deviceId: a.deviceId, deviceName: a.deviceName, title: `Acknowledged: ${a.title}`, message: `${actor} acknowledged the alert.` }
    state.events.unshift(ev)
    setTimeout(() => {
      broadcastAlert({ ...a })
      broadcastEvent(ev)
    }, 50)
    return json(a)
  }

  // ---- devices/{id}[/...] ---------------------------------------------------
  if (segs[0] === 'devices' && segs[1]) {
    const id = decodeURIComponent(segs[1])
    const sub = segs[2]
    refreshDevices(state, now)
    const d = findDevice(state, id)
    if (!d) return err('not_found', 'device not found', 404)
    const p = findProfile(d.id)!

    if (method === 'GET' && !sub) return json(deviceDetail(state, d, now))
    if (method === 'GET' && sub === 'series') {
      const r = params.get('range') ?? '1h'
      const range: RangeKey = isRangeKey(r) ? r : '1h'
      return json(buildSeries(p, range, now))
    }
    if (method === 'GET' && sub === 'uptime') {
      const r = params.get('range') ?? '24h'
      const range: RangeKey = isRangeKey(r) ? r : '24h'
      return json(buildUptime(p, range, now))
    }
    if (method === 'GET' && sub === 'events') {
      const limit = Math.min(500, Number(params.get('limit') ?? 50) || 50)
      return json(state.events.filter((e) => e.deviceId === d.id).slice(0, limit))
    }
    if (method === 'POST' && sub === 'ping') {
      await new Promise((r) => setTimeout(r, 300 + Math.random() * 500))
      const base = d.connectivity.latencyMs ?? 20
      const res: PingResult = d.online
        ? { deviceId: d.id, ip: d.addresses[0] ?? '', latencyMs: Math.round((base * (0.9 + Math.random() * 0.3)) * 100) / 100, path: d.connectivity.path, endpoint: d.connectivity.curAddr, relay: d.connectivity.relay, at: new Date().toISOString() }
        : { deviceId: d.id, ip: d.addresses[0] ?? '', latencyMs: 0, path: 'none', error: 'ping timeout after 10s', at: new Date().toISOString() }
      audit(actor, 'device.ping', d.id, { latencyMs: res.latencyMs, path: res.path }, !res.error, res.error)
      return json(res)
    }
    if (method === 'POST' && sub === 'authorize') {
      const body = await readBody<{ authorized?: boolean }>(init, input)
      const next = mutateDevice(d.id, { authorized: body?.authorized !== false })!
      audit(actor, 'device.authorize', d.id, { authorized: next.authorized })
      const open = state.alerts.find((a) => a.deviceId === d.id && a.ruleType === 'unauthorized_device' && a.state === 'open')
      if (next.authorized && open) {
        open.state = 'resolved'
        open.resolvedAt = new Date().toISOString()
        open.updatedAt = open.resolvedAt
        setTimeout(() => broadcastAlert({ ...open }), 50)
      }
      const ev: Event = { id: state.nextEventId++, ts: new Date().toISOString(), type: 'device.authorized', severity: 'info', deviceId: d.id, deviceName: next.name, title: `${next.name} authorized`, message: `${actor} authorized the device.` }
      state.events.unshift(ev)
      setTimeout(() => broadcastEvent(ev), 60)
      return json(next)
    }
    if (method === 'POST' && sub === 'tags') {
      const body = await readBody<{ tags?: string[] }>(init, input)
      const tags = Array.isArray(body?.tags) ? body.tags.filter((t) => typeof t === 'string') : null
      if (!tags) return err('bad_request', 'tags must be an array of strings', 400)
      if (tags.some((t) => !/^tag:[a-z0-9-]+$/i.test(t))) return err('bad_request', 'tags must look like tag:name', 400)
      const next = mutateDevice(d.id, { tags, user: tags.length ? 'tagged-devices' : d.user })!
      audit(actor, 'device.tags', d.id, { tags })
      return json(next)
    }
    if (method === 'POST' && sub === 'key-expiry') {
      const body = await readBody<{ disabled?: boolean }>(init, input)
      const disabled = !!body?.disabled
      const next = mutateDevice(d.id, { keyExpiryDisabled: disabled, keyExpiry: disabled ? undefined : d.keyExpiry })!
      audit(actor, 'device.key-expiry', d.id, { disabled })
      return json(next)
    }
    if (method === 'POST' && sub === 'routes') {
      const body = await readBody<{ routes?: string[] }>(init, input)
      const routes = Array.isArray(body?.routes) ? body.routes.filter((t) => typeof t === 'string') : null
      if (!routes) return err('bad_request', 'routes must be an array of CIDRs', 400)
      const next = mutateDevice(d.id, { enabledRoutes: routes, primaryRoutes: routes })!
      audit(actor, 'device.routes', d.id, { routes })
      return json(next)
    }
    if (method === 'POST' && sub === 'name') {
      const body = await readBody<{ name?: string }>(init, input)
      const name = (body?.name ?? '').trim().toLowerCase()
      if (!/^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/.test(name)) return err('bad_request', 'name must be a valid DNS label', 400)
      const next = mutateDevice(d.id, { name, dnsName: `${name}.${state.hub.magicDnsSuffix}` })!
      audit(actor, 'device.name', d.id, { name })
      return json(next)
    }
    if (method === 'DELETE' && !sub) {
      if (d.isSelf) return err('bad_request', 'cannot delete the hub node', 400)
      state.deleted.add(d.id)
      refreshDevices(state, now)
      audit(actor, 'device.delete', d.id)
      const ev: Event = { id: state.nextEventId++, ts: new Date().toISOString(), type: 'device.removed', severity: 'warning', deviceId: d.id, deviceName: d.name, title: `${d.name} removed`, message: `${actor} removed the device from the tailnet.` }
      state.events.unshift(ev)
      setTimeout(() => {
        broadcastEvent(ev)
        broadcastTick()
      }, 60)
      return new Response(null, { status: 204 })
    }
  }

  return err('not_found', `no route for ${method} ${path}`, 404)
}

async function mockFetch(realFetch: typeof fetch, input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  const rawUrl = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
  let url: URL
  try {
    url = new URL(rawUrl, window.location.origin)
  } catch {
    return realFetch(input, init)
  }
  if (url.origin !== window.location.origin) return realFetch(input, init)
  if (url.pathname === '/healthz') return new Response('ok', { status: 200, headers: { 'content-type': 'text/plain' } })
  if (!url.pathname.startsWith(API + '/') && url.pathname !== API) return realFetch(input, init)

  const method = (init?.method ?? (input instanceof Request ? input.method : 'GET')).toUpperCase()
  await delay()
  if (method !== 'GET' && headerValue(init, input, 'X-Requested-With') !== 'tailwatch') {
    return err('forbidden', 'missing X-Requested-With header', 403)
  }
  const path = url.pathname.slice(API.length) || '/'
  try {
    return await route(method, path, url.searchParams, init, input)
  } catch (e) {
    console.error('[mock] handler error', e)
    return err('internal', e instanceof Error ? e.message : 'mock failure', 500)
  }
}

// ---------------------------------------------------------------------------
// Fake EventSource
// ---------------------------------------------------------------------------

class MockEventSource extends EventTarget {
  static readonly CONNECTING = 0
  static readonly OPEN = 1
  static readonly CLOSED = 2
  readonly CONNECTING = 0
  readonly OPEN = 1
  readonly CLOSED = 2
  readonly url: string
  readonly withCredentials = false
  readyState = 0
  onopen: ((ev: globalThis.Event) => void) | null = null
  onmessage: ((ev: MessageEvent) => void) | null = null
  onerror: ((ev: globalThis.Event) => void) | null = null
  private openTimer: ReturnType<typeof setTimeout>

  constructor(url: string | URL, _init?: EventSourceInit) {
    super()
    this.url = typeof url === 'string' ? url : url.href
    sources.add(this)
    this.openTimer = setTimeout(() => this.open(), 250 + Math.random() * 250)
  }

  private open() {
    if (this.readyState === 2) return
    this.readyState = 1
    const ev = new globalThis.Event('open')
    this.dispatchEvent(ev)
    this.onopen?.(ev)
    const hello: SSEHello = { identity: state.identity, hub: state.hub }
    this.emit('hello', hello)
  }

  emit(type: string, data: unknown) {
    if (this.readyState !== 1) return
    const ev = new MessageEvent(type, { data: JSON.stringify(data) })
    this.dispatchEvent(ev)
    if (type === 'message') this.onmessage?.(ev)
  }

  /** Simulate a dropped connection: error → reconnect after `downMs`. */
  drop(downMs = 3000) {
    if (this.readyState === 2) return
    this.readyState = 0
    const ev = new globalThis.Event('error')
    this.dispatchEvent(ev)
    this.onerror?.(ev)
    clearTimeout(this.openTimer)
    this.openTimer = setTimeout(() => this.open(), downMs)
  }

  close() {
    this.readyState = 2
    clearTimeout(this.openTimer)
    sources.delete(this)
  }
}

// ---------------------------------------------------------------------------

export interface MockHandle {
  state: MockState
  /** Drop every live stream for `ms` (default 3s; use > 30000 to see the polling fallback). */
  dropStream: (ms?: number) => void
  /** Force an immediate tick broadcast. */
  tick: () => void
}

declare global {
  interface Window {
    __tailwatchMock?: MockHandle
  }
}

/** Install the fetch + EventSource shim. Safe to call more than once. */
export function installMock(): MockHandle {
  if (installed && window.__tailwatchMock) return window.__tailwatchMock
  installed = true
  state = createMockState()
  const realFetch = window.fetch.bind(window)
  window.fetch = ((input: RequestInfo | URL, init?: RequestInit) => mockFetch(realFetch, input, init)) as typeof fetch
  ;(window as unknown as { EventSource: unknown }).EventSource = MockEventSource
  ticker = setInterval(() => {
    const r = advance(state, Date.now())
    const tick: SSETick = { overview: r.overview, devices: r.devices }
    for (const es of sources) {
      es.emit('tick', tick)
      for (const e of r.events) es.emit('event', e)
      for (const a of r.alerts) es.emit('alert', a)
    }
  }, TICK_MS)
  const handle: MockHandle = {
    state,
    dropStream: (ms = 3000) => {
      for (const es of Array.from(sources)) es.drop(ms)
    },
    tick: broadcastTick,
  }
  window.__tailwatchMock = handle
  console.info('%c[tailwatch] mock API installed — window.__tailwatchMock', 'color:#4f8ef7')
  return handle
}

/** Remove the shim (tests). */
export function uninstallMock(): void {
  if (ticker) clearInterval(ticker)
  ticker = null
  installed = false
  for (const es of Array.from(sources)) es.close()
}
