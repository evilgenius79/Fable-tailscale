// Pure helpers for the Overview page (unit-tested in overview.test.ts).

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
import type { Alert, Device, EventType, OverviewSparklines, Severity } from '../../api/types'
import { formatDuration } from '../../lib/format'
import { osInfo } from '../../lib/os'
import { RANGE_PRESETS, type RangeKey } from '../../lib/time'

/** Smallest range preset that covers `seconds` (used to pick tick formatting for the sparkline chart). */
export function rangeForSpan(seconds: number): RangeKey {
  for (const p of RANGE_PRESETS) if (p.seconds >= seconds) return p.key
  return RANGE_PRESETS[RANGE_PRESETS.length - 1]!.key
}

export interface SparklineWindow {
  /** Seconds between the first and last point (0 when fewer than two points). */
  span: number
  /** Seconds between consecutive points (0 when unknown). */
  step: number
  range: RangeKey
}

export function sparklineWindow(s: OverviewSparklines | undefined): SparklineWindow {
  const t = s?.t ?? []
  if (t.length < 2) return { span: 0, step: 0, range: '1h' }
  const first = t[0]!
  const last = t[t.length - 1]!
  const span = Math.max(0, last - first)
  const step = t.length > 1 ? Math.round(span / (t.length - 1)) : 0
  return { span, step, range: rangeForSpan(span) }
}

/** Short comparison label for a sparkline window: 85 500 s → "vs 1d ago" (rounded to the hour beyond 1h). */
export function windowLabel(spanSeconds: number): string | undefined {
  if (!(spanSeconds > 0)) return undefined
  const rounded = spanSeconds >= 3600 ? Math.round(spanSeconds / 3600) * 3600 : Math.round(spanSeconds / 60) * 60
  return `vs ${formatDuration(rounded, 1)} ago`
}

/** Element-wise sum of two equally long numeric arrays (missing entries become null). */
export function sumSeries(a: ReadonlyArray<number>, b: ReadonlyArray<number>): (number | null)[] {
  const n = Math.max(a.length, b.length)
  const out: (number | null)[] = []
  for (let i = 0; i < n; i++) {
    const x = a[i]
    const y = b[i]
    out.push(typeof x === 'number' && typeof y === 'number' ? x + y : null)
  }
  return out
}

/** Signed change between the first and last finite value of a series (null when unavailable). */
export function deltaVsStart(values: ReadonlyArray<number | null | undefined>): number | null {
  const finite = values.filter((v): v is number => typeof v === 'number' && Number.isFinite(v))
  if (finite.length < 2) return null
  return finite[finite.length - 1]! - finite[0]!
}

/** Percent change between first and last finite value; null when the start is 0. */
export function percentDeltaVsStart(values: ReadonlyArray<number | null | undefined>): number | null {
  const finite = values.filter((v): v is number => typeof v === 'number' && Number.isFinite(v))
  if (finite.length < 2) return null
  const start = finite[0]!
  if (start === 0) return null
  return ((finite[finite.length - 1]! - start) / start) * 100
}

export type TopDevicesBy = 'throughput' | 'latency'

/** Online devices ranked by live throughput (rx+tx, desc) or latency (desc — worst first). */
export function topDevices(devices: ReadonlyArray<Device>, by: TopDevicesBy, limit = 8): Device[] {
  const online = devices.filter((d) => d.online)
  const score = (d: Device) => (by === 'throughput' ? d.connectivity.rxRate + d.connectivity.txRate : d.connectivity.latencyMs ?? -1)
  return [...online].sort((a, b) => score(b) - score(a) || a.name.localeCompare(b.name)).slice(0, limit)
}

/** Merge the raw `osBreakdown` keys ("linux", "macOS", "iOS", …) into display families. */
export function osSlices(breakdown: Record<string, number> | undefined): { label: string; value: number }[] {
  const map = new Map<string, number>()
  for (const [os, n] of Object.entries(breakdown ?? {})) {
    const label = osInfo(os).label
    map.set(label, (map.get(label) ?? 0) + n)
  }
  return Array.from(map, ([label, value]) => ({ label, value })).sort((a, b) => b.value - a.value || a.label.localeCompare(b.label))
}

export function userItems(breakdown: Record<string, number> | undefined): { label: string; value: number }[] {
  return Object.entries(breakdown ?? {})
    .map(([label, value]) => ({ label, value }))
    .sort((a, b) => b.value - a.value || a.label.localeCompare(b.label))
}

const SEVERITY_ORDER: Record<Severity, number> = { critical: 0, warning: 1, info: 2 }

/** Open alerts: unacknowledged first, then by severity, then newest. */
export function sortOpenAlerts(alerts: ReadonlyArray<Alert>): Alert[] {
  return [...alerts]
    .filter((a) => a.state === 'open')
    .sort((a, b) => {
      const ack = Number(!!a.ackedAt) - Number(!!b.ackedAt)
      if (ack) return ack
      const sev = SEVERITY_ORDER[a.severity] - SEVERITY_ORDER[b.severity]
      if (sev) return sev
      return b.openedAt.localeCompare(a.openedAt)
    })
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

/** Ratio helper that survives an empty fleet. */
export function percentOf(part: number, total: number): number | null {
  if (!total) return null
  return (part / total) * 100
}
