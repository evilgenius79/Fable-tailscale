// Pure helpers for the device detail page: series → chart rows, sparkline
// trends, DERP/NAT/route rows, uptime timeline geometry, badge derivation and
// input validation for the admin dialogs. No React, fully unit-tested.

import type { Device, MetricsSnapshot, NATSupport, PingResult, SeriesPoint, UptimeReport } from '../../api/types'
import { formatLatency, formatTime } from '../../lib/format'
import { pathLabel, type StatusTone } from '../../lib/status'

// ---------------------------------------------------------------------------
// Ping
// ---------------------------------------------------------------------------

/** Toast contents for a ping result; the id is per device so a re-ping replaces the previous toast. */
export function pingResultToast(device: Pick<Device, 'id' | 'name'>, r: PingResult): { id: string; tone: 'success' | 'error'; title: string; description?: string } {
  const id = `ping-${device.id}`
  if (r.error) return { id, tone: 'error', title: `Ping failed · ${device.name}`, description: r.error }
  const parts = [formatLatency(r.latencyMs), pathLabel(r.path, r.relay), r.endpoint, formatTime(r.at, true)].filter((s): s is string => !!s)
  return { id, tone: 'success', title: `Ping · ${device.name}`, description: parts.join(' · ') }
}

// ---------------------------------------------------------------------------
// Validation (admin dialogs)
// ---------------------------------------------------------------------------

/** Tailscale ACL tag: `tag:` + [a-z0-9-]. */
export const TAG_RE = /^tag:[a-z0-9-]+$/

/** RFC 1123 DNS label (what the control API accepts for the machine name). */
export const DNS_LABEL_RE = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/

/** Trim, lowercase and add the `tag:` prefix when missing ("Server" → "tag:server"). */
export function normalizeTag(input: string): string {
  const s = input.trim().toLowerCase()
  if (!s) return ''
  return s.startsWith('tag:') ? s : `tag:${s}`
}

/** Error message for a candidate tag, or null when it is acceptable. */
export function validateTag(tag: string, existing: ReadonlyArray<string> = []): string | null {
  if (!tag) return 'Enter a tag name'
  if (!TAG_RE.test(tag)) return 'Tags look like tag:name (letters, digits and dashes)'
  if (existing.includes(tag)) return 'That tag is already on this device'
  return null
}

/** Error message for a machine name, or null when it is a valid DNS label. */
export function validateDeviceName(name: string, current?: string): string | null {
  const s = name.trim().toLowerCase()
  if (!s) return 'Enter a name'
  if (s.length > 63) return 'Names are limited to 63 characters'
  if (!DNS_LABEL_RE.test(s)) return 'Use lowercase letters, digits and dashes (not at the ends)'
  if (current && s === current.toLowerCase()) return 'That is already the device name'
  return null
}

// ---------------------------------------------------------------------------
// Series
// ---------------------------------------------------------------------------

export type SeriesKey = Exclude<keyof SeriesPoint, 't'>

/** Chart rows: every numeric field of a point, keyed by its API name. */
export function seriesRows(points: ReadonlyArray<SeriesPoint>): Array<{ t: number; [key: string]: number | null | undefined }> {
  return points.map((p) => ({
    t: p.t,
    online: p.online,
    direct: p.direct,
    latencyMs: p.latencyMs,
    latencyMax: p.latencyMax,
    tsRxRate: p.tsRxRate,
    tsTxRate: p.tsTxRate,
    cpu: p.cpu,
    cpuMax: p.cpuMax,
    mem: p.mem,
    disk: p.disk,
    load1: p.load1,
    netRxRate: p.netRxRate,
    netTxRate: p.netTxRate,
    tempC: p.tempC,
  }))
}

/** True when at least one point carries a finite value for `key`. */
export function seriesHas(points: ReadonlyArray<SeriesPoint>, key: SeriesKey): boolean {
  return points.some((p) => typeof p[key] === 'number' && Number.isFinite(p[key]))
}

/**
 * Down-sample a series field to at most `n` evenly spaced values for a
 * sparkline. Missing values stay null so gaps render as gaps.
 */
export function trend(points: ReadonlyArray<SeriesPoint>, key: SeriesKey, n = 48): Array<number | null> {
  if (!points.length) return []
  const step = Math.max(1, Math.ceil(points.length / n))
  const out: Array<number | null> = []
  for (let i = points.length - 1; i >= 0; i -= step) {
    const v = points[i]![key]
    out.unshift(typeof v === 'number' && Number.isFinite(v) ? v : null)
  }
  return out
}

// ---------------------------------------------------------------------------
// Connectivity rows
// ---------------------------------------------------------------------------

export interface DerpRow {
  code: string
  name: string
  latencyMs: number
  preferred: boolean
}

/** DERP regions sorted by latency (preferred region flagged). */
export function derpRows(latency: Record<string, number> | undefined, preferred: string | undefined, regions: Record<string, string> = {}): DerpRow[] {
  if (!latency) return []
  return Object.entries(latency)
    .filter(([, v]) => typeof v === 'number' && Number.isFinite(v))
    .map(([code, latencyMs]) => ({ code, name: regions[code] ?? code, latencyMs, preferred: code === preferred }))
    .sort((a, b) => a.latencyMs - b.latencyMs || a.code.localeCompare(b.code))
}

export interface NatRow {
  id: string
  label: string
  /** null = not reported. */
  supported: boolean | null
  /** What the capability means for path selection. */
  detail: string
  /** Whether `supported === true` is the good outcome (mapping variance is the inverse). */
  goodWhenTrue: boolean
}

/** The NAT traversal capability matrix reported by the control API. */
export function natRows(nat: NATSupport | undefined, mappingVaries: boolean | undefined): NatRow[] {
  const v = (k: keyof NATSupport) => (nat ? nat[k] : null)
  return [
    { id: 'udp', label: 'UDP', supported: v('udp'), detail: 'Required for direct connections', goodWhenTrue: true },
    { id: 'ipv6', label: 'IPv6', supported: v('ipv6'), detail: 'Direct paths over IPv6', goodWhenTrue: true },
    { id: 'upnp', label: 'UPnP', supported: v('upnp'), detail: 'Router port mapping', goodWhenTrue: true },
    { id: 'pmp', label: 'NAT-PMP', supported: v('pmp'), detail: 'Apple port mapping', goodWhenTrue: true },
    { id: 'pcp', label: 'PCP', supported: v('pcp'), detail: 'Port Control Protocol', goodWhenTrue: true },
    { id: 'hairpin', label: 'Hairpinning', supported: v('hairPinning'), detail: 'Peers behind the same NAT', goodWhenTrue: true },
    {
      id: 'mapping',
      label: 'Mapping varies by destination',
      supported: typeof mappingVaries === 'boolean' ? mappingVaries : null,
      detail: 'Hard NAT; direct paths are unlikely',
      goodWhenTrue: false,
    },
  ]
}

export interface RouteRow {
  route: string
  /** Approved by an admin. */
  enabled: boolean
  /** Currently the primary router for this prefix. */
  primary: boolean
  /** A default route (exit node). */
  exit: boolean
}

export function isExitRoute(route: string): boolean {
  return route === '0.0.0.0/0' || route === '::/0'
}

/** Advertised routes with their enabled/primary state; enabled-but-not-advertised routes are included too. */
export function routeRows(d: Pick<Device, 'advertisedRoutes' | 'enabledRoutes' | 'primaryRoutes'>): RouteRow[] {
  const all = Array.from(new Set([...(d.advertisedRoutes ?? []), ...(d.enabledRoutes ?? [])]))
  const enabled = new Set(d.enabledRoutes ?? [])
  const primary = new Set(d.primaryRoutes ?? [])
  return all
    .map((route) => ({ route, enabled: enabled.has(route), primary: primary.has(route), exit: isExitRoute(route) }))
    .sort((a, b) => Number(a.exit) - Number(b.exit) || a.route.localeCompare(b.route, undefined, { numeric: true }))
}

// ---------------------------------------------------------------------------
// Availability timeline
// ---------------------------------------------------------------------------

export interface TimelineBar {
  /** Unix ms. */
  from: number
  to: number
  online: boolean
  /** Percent offset/width across the report window. */
  startPct: number
  widthPct: number
  seconds: number
}

const ms = (s: string) => new Date(s).getTime()

/** Segment geometry for the availability bar (empty when the report is unusable). */
export function timelineBars(report: Pick<UptimeReport, 'from' | 'to' | 'segments'> | undefined): TimelineBar[] {
  if (!report) return []
  const start = ms(report.from)
  const end = ms(report.to)
  const span = end - start
  if (!Number.isFinite(span) || span <= 0) return []
  return report.segments
    .map((s) => {
      const f = Math.max(start, ms(s.from))
      const t = Math.min(end, ms(s.to))
      return { from: f, to: t, online: s.online, startPct: ((f - start) / span) * 100, widthPct: Math.max(0, ((t - f) / span) * 100), seconds: Math.max(0, (t - f) / 1000) }
    })
    .filter((b) => Number.isFinite(b.startPct) && b.to > b.from)
}

export interface Outage {
  from: number
  to: number
  seconds: number
  /** True when the outage runs to the end of the window (still down). */
  ongoing: boolean
}

/** Offline segments, newest first, with the longest first when `byLength` is set. */
export function outageList(report: Pick<UptimeReport, 'from' | 'to' | 'segments'> | undefined, byLength = false): Outage[] {
  const bars = timelineBars(report)
  if (!bars.length) return []
  const end = ms(report!.to)
  const out = bars.filter((b) => !b.online).map((b) => ({ from: b.from, to: b.to, seconds: b.seconds, ongoing: b.to >= end }))
  return byLength ? out.sort((a, b) => b.seconds - a.seconds) : out.sort((a, b) => b.from - a.from)
}

/** Evenly spaced tick timestamps (unix ms) across a window, including both ends. */
export function timelineTicks(from: string, to: string, count = 5): number[] {
  const start = ms(from)
  const end = ms(to)
  if (!Number.isFinite(start) || !Number.isFinite(end) || end <= start || count < 2) return []
  const out: number[] = []
  for (let i = 0; i < count; i++) out.push(start + ((end - start) * i) / (count - 1))
  return out
}

// ---------------------------------------------------------------------------
// Header badges & agent
// ---------------------------------------------------------------------------

export interface DeviceBadgeSpec {
  id: string
  label: string
  tone: StatusTone
}

/**
 * Role/state badges shown next to the device name. The headline status badge
 * (from deviceStatus) already covers unauthorized/expired, so they are not
 * repeated here.
 */
export function deviceBadges(d: Device): DeviceBadgeSpec[] {
  const out: DeviceBadgeSpec[] = []
  if (d.isSelf) out.push({ id: 'self', label: 'This hub', tone: 'accent' })
  if (d.isExitNode) out.push({ id: 'exit', label: 'Exit node', tone: 'info' })
  else if (d.exitNodeOption) out.push({ id: 'exit-offered', label: 'Offers exit node', tone: 'neutral' })
  if ((d.enabledRoutes ?? []).some((r) => !isExitRoute(r))) out.push({ id: 'router', label: 'Subnet router', tone: 'info' })
  else if ((d.advertisedRoutes ?? []).some((r) => !isExitRoute(r))) out.push({ id: 'router-pending', label: 'Routes pending', tone: 'warning' })
  if (d.isExternal) out.push({ id: 'shared', label: 'Shared in', tone: 'neutral' })
  if (d.updateAvailable) out.push({ id: 'update', label: 'Update available', tone: 'info' })
  if (d.keyExpiryDisabled) out.push({ id: 'noexpiry', label: 'Key expiry off', tone: 'neutral' })
  return out
}

/** Shell snippet for installing the agent (mirrors docs/AGENT.md). */
export function agentInstallCommand(port: number): string {
  const portFlag = port && port !== 41820 ? ` --port ${port}` : ''
  return `curl -fsSLO https://raw.githubusercontent.com/evilgenius79/fable-tailscale/main/scripts/install-agent.sh\nsudo sh install-agent.sh --allow-tag tag:tailwatch-hub${portFlag}`
}

/** True when the device has a metrics snapshot worth rendering. */
export function hasMetrics(d: Pick<Device, 'metrics'> | undefined): d is { metrics: MetricsSnapshot } & Pick<Device, 'metrics'> {
  return !!d?.metrics && typeof d.metrics.cpuCount === 'number'
}

/** Load average relative to the core count as a 0–100 percentage (null without cores). */
export function loadPercent(load1: number | undefined, cpuCount: number | undefined): number | null {
  if (typeof load1 !== 'number' || !cpuCount) return null
  return Math.max(0, (load1 / cpuCount) * 100)
}

/** Interface rows sorted with the Tailscale interface first, then by traffic. */
export function interfaceRows(m: Pick<MetricsSnapshot, 'interfaces' | 'tailscaleInterface'>): Array<MetricsSnapshot['interfaces'][number] & { tailscale: boolean }> {
  return (m.interfaces ?? [])
    .map((i) => ({ ...i, tailscale: i.name === m.tailscaleInterface }))
    .sort((a, b) => Number(b.tailscale) - Number(a.tailscale) || b.rxRate + b.txRate - (a.rxRate + a.txRate) || a.name.localeCompare(b.name))
}
