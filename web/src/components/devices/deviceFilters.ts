// Pure filter / sort / URL helpers for the Devices page. No React, no DOM —
// everything here is unit-tested in deviceFilters.test.ts.

import type { Device, PathType } from '../../api/types'
import { daysUntil, tagLabel } from '../../lib/format'
import { osFamily, osInfo, type OSFamily } from '../../lib/os'
import { deviceStatus } from '../../lib/status'
import { sortRows, type SortDir, type SortState, type SortValue } from '../ui/Table'

export type StatusFilter = 'all' | 'online' | 'offline'
export type PathFilter = 'all' | 'direct' | 'relay'
export type DeviceFlag = 'update' | 'keyExpiring' | 'unauthorized' | 'exitNode' | 'subnetRouter' | 'agent'

export const STATUS_OPTIONS: ReadonlyArray<{ value: StatusFilter; label: string }> = [
  { value: 'all', label: 'All' },
  { value: 'online', label: 'Online' },
  { value: 'offline', label: 'Offline' },
]

export const PATH_OPTIONS: ReadonlyArray<{ value: PathFilter; label: string }> = [
  { value: 'all', label: 'Any path' },
  { value: 'direct', label: 'Direct' },
  { value: 'relay', label: 'Relay' },
]

export const FLAGS: readonly DeviceFlag[] = ['update', 'keyExpiring', 'unauthorized', 'exitNode', 'subnetRouter', 'agent']

export const FLAG_META: Record<DeviceFlag, { label: string; description: string }> = {
  update: { label: 'Update available', description: 'A newer Tailscale client is available' },
  keyExpiring: { label: 'Key expiring ≤7d', description: 'Node key expires within seven days' },
  unauthorized: { label: 'Unauthorized', description: 'Waiting for admin approval' },
  exitNode: { label: 'Exit node', description: 'Advertises itself as an exit node' },
  subnetRouter: { label: 'Subnet router', description: 'Has at least one enabled subnet route' },
  agent: { label: 'Agent reachable', description: 'tailwatch-agent is responding' },
}

export const OS_FAMILIES: readonly OSFamily[] = ['linux', 'macos', 'windows', 'ios', 'android', 'freebsd', 'openbsd', 'tvos', 'other']

export interface DeviceFilters {
  q: string
  status: StatusFilter
  path: PathFilter
  os: OSFamily[]
  user: string
  tag: string
  flags: DeviceFlag[]
}

export const DEFAULT_FILTERS: DeviceFilters = { q: '', status: 'all', path: 'all', os: [], user: '', tag: '', flags: [] }
export const DEFAULT_SORT: SortState = { key: 'name', dir: 'asc' }

/** Days-until-expiry at or below which a key counts as "expiring". */
export const KEY_EXPIRING_DAYS = 7

const isDefaultRoute = (r: string) => r.startsWith('0.0.0.0/') || r.startsWith('::/')

/** True when the device has an enabled non-default route (subnet router). */
export function isSubnetRouter(d: Device): boolean {
  return d.enabledRoutes.some((r) => !isDefaultRoute(r))
}

/** Days until the node key expires; null when disabled or unknown. */
export function keyExpiryDays(d: Device, now: number = Date.now()): number | null {
  if (!d.keyExpiry || d.keyExpiryDisabled) return null
  return daysUntil(d.keyExpiry, now)
}

/** Key expires within KEY_EXPIRING_DAYS (already-expired keys are reported by `expired`, not here). */
export function isKeyExpiring(d: Device, now: number = Date.now()): boolean {
  if (d.expired) return false
  const days = keyExpiryDays(d, now)
  return days !== null && days <= KEY_EXPIRING_DAYS
}

export function hasFlag(d: Device, flag: DeviceFlag, now: number = Date.now()): boolean {
  switch (flag) {
    case 'update':
      return d.updateAvailable
    case 'keyExpiring':
      return isKeyExpiring(d, now)
    case 'unauthorized':
      return !d.authorized
    case 'exitNode':
      return d.isExitNode
    case 'subnetRouter':
      return isSubnetRouter(d)
    case 'agent':
      return d.agent.state === 'reachable'
  }
}

/** Searchable text for a device: name, hostname, DNS name, IPs, user, tags, OS, model. */
export function searchHaystack(d: Device): string {
  return [
    d.name,
    d.hostname,
    d.dnsName,
    ...d.addresses,
    d.user,
    d.userDisplayName ?? '',
    ...d.tags,
    ...d.tags.map(tagLabel),
    d.os,
    osInfo(d.os).label,
    d.deviceModel ?? '',
    d.clientVersion ?? '',
    d.connectivity.relay ?? '',
  ]
    .join('\n')
    .toLowerCase()
}

/** Every whitespace-separated token of `q` must appear somewhere in the haystack. */
export function matchesSearch(d: Device, q: string): boolean {
  const tokens = q.trim().toLowerCase().split(/\s+/).filter(Boolean)
  if (!tokens.length) return true
  const hay = searchHaystack(d)
  return tokens.every((t) => hay.includes(t))
}

export function matchesFilters(d: Device, f: DeviceFilters, now: number = Date.now()): boolean {
  if (f.status === 'online' && !d.online) return false
  if (f.status === 'offline' && d.online) return false
  if (f.path !== 'all' && d.connectivity.path !== f.path) return false
  if (f.os.length && !f.os.includes(osFamily(d.os))) return false
  if (f.user && d.user !== f.user) return false
  if (f.tag && !d.tags.includes(f.tag)) return false
  for (const flag of f.flags) if (!hasFlag(d, flag, now)) return false
  return matchesSearch(d, f.q)
}

export function applyFilters(devices: ReadonlyArray<Device>, f: DeviceFilters, now: number = Date.now()): Device[] {
  return devices.filter((d) => matchesFilters(d, f, now))
}

/** Number of active non-search filters (each flag and each OS family counts once). */
export function countActiveFilters(f: DeviceFilters): number {
  let n = 0
  if (f.status !== 'all') n++
  if (f.path !== 'all') n++
  n += f.os.length
  if (f.user) n++
  if (f.tag) n++
  n += f.flags.length
  return n
}

export function hasAnyFilter(f: DeviceFilters): boolean {
  return countActiveFilters(f) > 0 || f.q.trim().length > 0
}

// ---------------------------------------------------------------------------
// URL (de)serialisation — only non-default values are written.
// ---------------------------------------------------------------------------

const isStatus = (v: string | null): v is StatusFilter => v === 'online' || v === 'offline'
const isPath = (v: string | null): v is PathFilter => v === 'direct' || v === 'relay'
const isFlag = (v: string): v is DeviceFlag => (FLAGS as readonly string[]).includes(v)
const isFamily = (v: string): v is OSFamily => (OS_FAMILIES as readonly string[]).includes(v)

function list(sp: URLSearchParams, key: string): string[] {
  const out: string[] = []
  for (const raw of sp.getAll(key)) for (const v of raw.split(',')) if (v.trim()) out.push(v.trim())
  return Array.from(new Set(out))
}

export function parseFilters(sp: URLSearchParams): DeviceFilters {
  const status = sp.get('status')
  const path = sp.get('path')
  return {
    q: (sp.get('q') ?? '').slice(0, 200),
    status: isStatus(status) ? status : 'all',
    path: isPath(path) ? path : 'all',
    os: list(sp, 'os').filter(isFamily),
    user: (sp.get('user') ?? '').slice(0, 200),
    tag: (sp.get('tag') ?? '').slice(0, 200),
    flags: list(sp, 'flags').filter(isFlag),
  }
}

export function parseSort(sp: URLSearchParams, allowedKeys: ReadonlyArray<string>): SortState {
  const raw = sp.get('sort')
  if (!raw) return DEFAULT_SORT
  const [key, dirRaw] = raw.split(':')
  if (!key || !allowedKeys.includes(key)) return DEFAULT_SORT
  const dir: SortDir = dirRaw === 'desc' ? 'desc' : 'asc'
  return { key, dir }
}

/** Serialise filters + sort into query params, omitting defaults so URLs stay short. */
export function serializeView(f: DeviceFilters, sort: SortState | null): URLSearchParams {
  const sp = new URLSearchParams()
  if (f.q.trim()) sp.set('q', f.q.trim())
  if (f.status !== 'all') sp.set('status', f.status)
  if (f.path !== 'all') sp.set('path', f.path)
  if (f.os.length) sp.set('os', f.os.join(','))
  if (f.user) sp.set('user', f.user)
  if (f.tag) sp.set('tag', f.tag)
  if (f.flags.length) sp.set('flags', f.flags.join(','))
  if (sort && (sort.key !== DEFAULT_SORT.key || sort.dir !== DEFAULT_SORT.dir)) sp.set('sort', `${sort.key}:${sort.dir}`)
  return sp
}

export function filtersEqual(a: DeviceFilters, b: DeviceFilters): boolean {
  return serializeView(a, null).toString() === serializeView(b, null).toString()
}

// ---------------------------------------------------------------------------
// Sorting
// ---------------------------------------------------------------------------

const PATH_RANK: Record<PathType, number> = { direct: 0, relay: 1, unknown: 2, none: 3 }

/** Headline-status rank for sorting: attention first, then online, then offline. */
export function statusRank(d: Device): number {
  const tone = deviceStatus(d).tone
  if (tone === 'critical') return 0
  if (tone === 'warning') return 1
  if (tone === 'offline') return 3
  return 2
}

/** Zero-padded IPv4 so string comparison orders numerically; IPv6 falls back to the raw string. */
export function ipSortKey(addr: string | undefined): string | null {
  if (!addr) return null
  const m = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/.exec(addr)
  if (!m) return `~${addr}`
  return m
    .slice(1)
    .map((o) => o.padStart(3, '0'))
    .join('.')
}

const rate = (d: Device, k: 'rxRate' | 'txRate'): number | null => (d.online ? d.connectivity[k] : null)

export const SORT_ACCESSORS: Record<string, (d: Device) => SortValue> = {
  name: (d) => d.name,
  status: (d) => statusRank(d),
  os: (d) => osInfo(d.os).label,
  ip: (d) => ipSortKey(d.addresses.find((a) => a.includes('.')) ?? d.addresses[0]),
  user: (d) => d.userDisplayName ?? d.user,
  path: (d) => (d.online ? PATH_RANK[d.connectivity.path] : PATH_RANK.none),
  latency: (d) => (d.online ? d.connectivity.latencyMs : null),
  rx: (d) => rate(d, 'rxRate'),
  tx: (d) => rate(d, 'txRate'),
  throughput: (d) => (d.online ? d.connectivity.rxRate + d.connectivity.txRate : null),
  cpu: (d) => d.metrics?.cpuPercent,
  mem: (d) => d.metrics?.memPercent,
  disk: (d) => d.metrics?.diskPercent,
  uptime: (d) => d.uptime.pct24h,
  version: (d) => d.clientVersion,
  keyExpiry: (d) => keyExpiryDays(d),
  lastSeen: (d) => Date.parse(d.lastSeen) || null,
}

export const SORT_KEYS: readonly string[] = Object.keys(SORT_ACCESSORS)

export function sortDevices(devices: ReadonlyArray<Device>, sort: SortState | null): Device[] {
  return sortRows(devices, sort ?? DEFAULT_SORT, SORT_ACCESSORS)
}

// ---------------------------------------------------------------------------
// Facets for the filter UI
// ---------------------------------------------------------------------------

export interface Facet<T extends string = string> {
  value: T
  label: string
  count: number
}

export function osFacets(devices: ReadonlyArray<Device>): Facet<OSFamily>[] {
  const counts = new Map<OSFamily, number>()
  for (const d of devices) {
    const fam = osFamily(d.os)
    counts.set(fam, (counts.get(fam) ?? 0) + 1)
  }
  return OS_FAMILIES.filter((f) => counts.has(f)).map((f) => ({
    value: f,
    label: f === 'other' ? 'Other' : osInfo(f).label,
    count: counts.get(f) ?? 0,
  }))
}

export function userFacets(devices: ReadonlyArray<Device>): Facet[] {
  const map = new Map<string, Facet>()
  for (const d of devices) {
    const cur = map.get(d.user)
    if (cur) cur.count++
    else map.set(d.user, { value: d.user, label: d.userDisplayName ?? d.user, count: 1 })
  }
  return Array.from(map.values()).sort((a, b) => b.count - a.count || a.label.localeCompare(b.label))
}

export function tagFacets(devices: ReadonlyArray<Device>): Facet[] {
  const map = new Map<string, Facet>()
  for (const d of devices)
    for (const t of d.tags) {
      const cur = map.get(t)
      if (cur) cur.count++
      else map.set(t, { value: t, label: t, count: 1 })
    }
  return Array.from(map.values()).sort((a, b) => b.count - a.count || a.value.localeCompare(b.value))
}

/** Sum of the fleet's live Tailscale rates for the result-count hint. */
export function totalRates(devices: ReadonlyArray<Device>): { rx: number; tx: number; online: number } {
  let rx = 0
  let tx = 0
  let online = 0
  for (const d of devices) {
    if (!d.online) continue
    online++
    rx += d.connectivity.rxRate
    tx += d.connectivity.txRate
  }
  return { rx, tx, online }
}
