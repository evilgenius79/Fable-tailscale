// Pure helpers for the Events page: type groups, icons, filters (URL
// round-trip), server query mapping, merge/dedupe of paginated + live pages,
// day grouping and the JSON export. No React; unit-tested in events.test.ts.

import type { LucideIcon } from 'lucide-react'
import {
  Bell,
  BellOff,
  Check,
  KeyRound,
  Plus,
  Power,
  Radio,
  RadioTower,
  RefreshCw,
  ShieldCheck,
  Trash,
  TriangleAlert,
  UserCog,
  Waypoints,
  Wifi,
  WifiOff,
} from 'lucide-react'
import type { EventsParams } from '../../api/queryKeys'
import type { Event, EventType, Severity } from '../../api/types'
import { dayKey, dayLabel } from '../alerts/alerts'

export { dayKey, dayLabel }

// ---------------------------------------------------------------------------
// Types & groups
// ---------------------------------------------------------------------------

export type EventGroupId = 'device' | 'agent' | 'alert' | 'admin' | 'hub'

export interface EventGroup {
  id: EventGroupId
  label: string
  types: readonly EventType[]
}

export const EVENT_GROUPS: readonly EventGroup[] = [
  {
    id: 'device',
    label: 'Device',
    types: ['device.online', 'device.offline', 'device.new', 'device.removed', 'device.updated', 'device.authorized', 'device.expired', 'device.path_changed'],
  },
  { id: 'agent', label: 'Agent', types: ['agent.reachable', 'agent.unreachable'] },
  { id: 'alert', label: 'Alert', types: ['alert.opened', 'alert.resolved', 'alert.acked'] },
  { id: 'admin', label: 'Admin', types: ['admin.action'] },
  { id: 'hub', label: 'Hub', types: ['hub.started', 'hub.error'] },
]

export const ALL_EVENT_TYPES: readonly EventType[] = EVENT_GROUPS.flatMap((g) => g.types)

export function isEventType(v: unknown): v is EventType {
  return typeof v === 'string' && (ALL_EVENT_TYPES as readonly string[]).includes(v)
}

export function eventGroupOf(type: EventType): EventGroupId {
  return (type.split('.')[0] as EventGroupId) ?? 'hub'
}

export const EVENT_ICONS: Record<EventType, LucideIcon> = {
  'device.online': Wifi,
  'device.offline': WifiOff,
  'device.new': Plus,
  'device.removed': Trash,
  'device.updated': RefreshCw,
  'device.authorized': ShieldCheck,
  'device.expired': KeyRound,
  'device.path_changed': Waypoints,
  'agent.reachable': Radio,
  'agent.unreachable': RadioTower,
  'alert.opened': Bell,
  'alert.resolved': BellOff,
  'alert.acked': Check,
  'admin.action': UserCog,
  'hub.started': Power,
  'hub.error': TriangleAlert,
}

export function eventIcon(type: EventType): LucideIcon {
  return EVENT_ICONS[type] ?? Bell
}

// ---------------------------------------------------------------------------
// Filters
// ---------------------------------------------------------------------------

export type SincePreset = '1h' | '24h' | '7d' | 'all'

export const SINCE_PRESETS: readonly { value: SincePreset; label: string; seconds: number | null }[] = [
  { value: '1h', label: '1h', seconds: 3600 },
  { value: '24h', label: '24h', seconds: 86400 },
  { value: '7d', label: '7d', seconds: 7 * 86400 },
  { value: 'all', label: 'All', seconds: null },
]

export function isSincePreset(v: unknown): v is SincePreset {
  return SINCE_PRESETS.some((p) => p.value === v)
}

/** RFC 3339 lower bound for a preset, or undefined for "all". */
export function sinceISO(preset: SincePreset, now: number = Date.now()): string | undefined {
  const p = SINCE_PRESETS.find((x) => x.value === preset)
  if (!p || p.seconds === null) return undefined
  return new Date(now - p.seconds * 1000).toISOString()
}

export type SeverityFilter = 'all' | Severity

export interface EventFilters {
  q: string
  /** Selected types; empty = every type. */
  types: EventType[]
  severity: SeverityFilter
  /** Device id ('' = any). */
  device: string
  since: SincePreset
}

export const DEFAULT_EVENT_FILTERS: EventFilters = { q: '', types: [], severity: 'all', device: '', since: '24h' }

export function hasEventFilters(f: EventFilters): boolean {
  return f.q.trim() !== '' || f.types.length > 0 || f.severity !== 'all' || f.device !== '' || f.since !== DEFAULT_EVENT_FILTERS.since
}

export function countActiveEventFilters(f: EventFilters): number {
  return (f.types.length ? 1 : 0) + (f.severity !== 'all' ? 1 : 0) + (f.device ? 1 : 0) + (f.since !== DEFAULT_EVENT_FILTERS.since ? 1 : 0)
}

/** Read filters from the query string; unknown values fall back to defaults. */
export function parseEventFilters(sp: URLSearchParams): EventFilters {
  const types = (sp.get('type') ?? '')
    .split(',')
    .map((s) => s.trim())
    .filter(isEventType)
  const sev = sp.get('severity')
  const since = sp.get('since')
  return {
    q: sp.get('q') ?? '',
    types: Array.from(new Set(types)),
    severity: sev === 'info' || sev === 'warning' || sev === 'critical' ? sev : 'all',
    device: sp.get('device') ?? '',
    since: isSincePreset(since) ? since : DEFAULT_EVENT_FILTERS.since,
  }
}

/** Write filters to a query string, omitting defaults so the URL stays short. */
export function serializeEventFilters(f: EventFilters): URLSearchParams {
  const sp = new URLSearchParams()
  if (f.q.trim()) sp.set('q', f.q)
  if (f.types.length) sp.set('type', f.types.join(','))
  if (f.severity !== 'all') sp.set('severity', f.severity)
  if (f.device) sp.set('device', f.device)
  if (f.since !== DEFAULT_EVENT_FILTERS.since) sp.set('since', f.since)
  return sp
}

/**
 * Server-side parameters for a filter set. `type` accepts a comma-separated
 * list (docs/API.md), so every selected type is sent and the page is already
 * narrowed by the hub; severity and free text are filtered client-side.
 */
export function eventQueryParams(f: EventFilters, limit: number, now: number = Date.now()): EventsParams {
  const p: EventsParams = { limit }
  if (f.types.length) p.type = f.types.join(',')
  if (f.device) p.device = f.device
  const since = sinceISO(f.since, now)
  if (since) p.since = since
  return p
}

/** Client-side filter (applied on top of what the server already narrowed). */
export function filterEvents(events: ReadonlyArray<Event>, f: EventFilters, now: number = Date.now()): Event[] {
  const q = f.q.trim().toLowerCase()
  const since = sinceISO(f.since, now)
  const types = f.types.length ? new Set<EventType>(f.types) : null
  return events.filter((e) => {
    if (types && !types.has(e.type)) return false
    if (f.severity !== 'all' && e.severity !== f.severity) return false
    if (f.device && e.deviceId !== f.device) return false
    if (since && e.ts < since) return false
    if (q) {
      const hay = `${e.title} ${e.message ?? ''} ${e.deviceName ?? ''} ${e.type}`.toLowerCase()
      if (!hay.includes(q)) return false
    }
    return true
  })
}

// ---------------------------------------------------------------------------
// Pages
// ---------------------------------------------------------------------------

/** Merge event lists (head page + older pages), dedupe by id, newest first. */
export function mergeEvents(...lists: ReadonlyArray<ReadonlyArray<Event>>): Event[] {
  const map = new Map<number, Event>()
  for (const list of lists) for (const e of list) if (!map.has(e.id)) map.set(e.id, e)
  return Array.from(map.values()).sort((a, b) => b.ts.localeCompare(a.ts) || b.id - a.id)
}

/** Ids in `page` that are not in `known`. */
export function newIds(page: ReadonlyArray<Event>, known: ReadonlySet<number>): number[] {
  return page.filter((e) => !known.has(e.id)).map((e) => e.id)
}

/** The oldest timestamp in a list (cursor for the next page), or undefined. */
export function oldestTs(events: ReadonlyArray<Event>): string | undefined {
  let min: string | undefined
  for (const e of events) if (min === undefined || e.ts < min) min = e.ts
  return min
}

/**
 * `before` value for the next older page. The hub stores timestamps at second
 * resolution and treats `before` as strictly exclusive, so asking for
 * `ts < oldest` would skip every event that shares the oldest event's second
 * (hub start, alert + audit pairs). Ask for `ts < oldest + 1s` instead and
 * let the id-based dedupe drop the ones already loaded.
 */
export function beforeCursor(oldest: string): string {
  const t = Date.parse(oldest)
  if (Number.isNaN(t)) return oldest
  return new Date(t + 1000).toISOString()
}

export interface EventDayGroup {
  key: string
  label: string
  events: Event[]
}

/** Group newest-first events by local calendar day, preserving order. */
export function groupEventsByDay(events: ReadonlyArray<Event>, now: Date = new Date()): EventDayGroup[] {
  const groups: EventDayGroup[] = []
  for (const e of events) {
    const key = dayKey(e.ts)
    const last = groups[groups.length - 1]
    if (last && last.key === key) last.events.push(e)
    else groups.push({ key, label: dayLabel(e.ts, now), events: [e] })
  }
  return groups
}

// ---------------------------------------------------------------------------
// Export
// ---------------------------------------------------------------------------

/** Pretty JSON of the visible events (API field names, newest first). */
export function eventsToJSON(events: ReadonlyArray<Event>, meta: { exportedAt: string; filters: EventFilters }): string {
  return JSON.stringify({ exportedAt: meta.exportedAt, count: events.length, filters: meta.filters, events }, null, 2)
}

/** "tailwatch-events-2026-09-28T12-05-00.json" */
export function exportFilename(now: number | Date = Date.now()): string {
  const iso = new Date(now).toISOString().replace(/\.\d{3}Z$/, '').replace(/:/g, '-')
  return `tailwatch-events-${iso}.json`
}
