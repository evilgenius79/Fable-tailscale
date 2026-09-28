// Time-range presets shared by the series/uptime endpoints and the picker.

export type RangeKey = '15m' | '1h' | '3h' | '6h' | '12h' | '24h' | '2d' | '7d' | '14d' | '30d'

export interface RangePreset {
  key: RangeKey
  /** Short label for segmented controls: "1h". */
  label: string
  /** Long label for menus: "Last hour". */
  longLabel: string
  /** Duration in seconds. */
  seconds: number
}

export const RANGE_PRESETS: readonly RangePreset[] = [
  { key: '15m', label: '15m', longLabel: 'Last 15 minutes', seconds: 15 * 60 },
  { key: '1h', label: '1h', longLabel: 'Last hour', seconds: 3600 },
  { key: '3h', label: '3h', longLabel: 'Last 3 hours', seconds: 3 * 3600 },
  { key: '6h', label: '6h', longLabel: 'Last 6 hours', seconds: 6 * 3600 },
  { key: '12h', label: '12h', longLabel: 'Last 12 hours', seconds: 12 * 3600 },
  { key: '24h', label: '24h', longLabel: 'Last 24 hours', seconds: 86400 },
  { key: '2d', label: '2d', longLabel: 'Last 2 days', seconds: 2 * 86400 },
  { key: '7d', label: '7d', longLabel: 'Last 7 days', seconds: 7 * 86400 },
  { key: '14d', label: '14d', longLabel: 'Last 14 days', seconds: 14 * 86400 },
  { key: '30d', label: '30d', longLabel: 'Last 30 days', seconds: 30 * 86400 },
] as const

/** The subset shown in compact pickers. */
export const DEFAULT_RANGE_OPTIONS: readonly RangeKey[] = ['1h', '6h', '24h', '7d', '30d']

export const DEFAULT_RANGE: RangeKey = '24h'

export function isRangeKey(v: unknown): v is RangeKey {
  return typeof v === 'string' && RANGE_PRESETS.some((p) => p.key === v)
}

export function rangePreset(key: RangeKey): RangePreset {
  return RANGE_PRESETS.find((p) => p.key === key) ?? RANGE_PRESETS[5]!
}

export function rangeSeconds(key: RangeKey): number {
  return rangePreset(key).seconds
}

/** The step (seconds) the series endpoint will use for a range (docs/API.md). */
export function rangeStepSeconds(key: RangeKey): number {
  const s = rangeSeconds(key)
  if (s <= 3 * 3600) return 15
  if (s <= 86400) return 60
  if (s <= 2 * 86400) return 300
  if (s <= 7 * 86400) return 300
  if (s <= 14 * 86400) return 1800
  return 3600
}

/** How often a series query for this range should refetch (ms). */
export function rangeRefetchMs(key: RangeKey): number {
  const s = rangeSeconds(key)
  if (s <= 3 * 3600) return 30_000
  if (s <= 86400) return 60_000
  return 5 * 60_000
}

/** Whether tick labels across this range should show the date rather than just the time. */
export function rangeShowsDate(key: RangeKey): boolean {
  return rangeSeconds(key) > 86400
}

/**
 * Format an axis tick (unix seconds) for the given range: times inside a day,
 * "Mon 14:00" for 2–7 days, "Sep 28" beyond.
 */
export function formatTick(unixSeconds: number, key: RangeKey): string {
  const d = new Date(unixSeconds * 1000)
  const s = rangeSeconds(key)
  if (s <= 86400) {
    return new Intl.DateTimeFormat('en-US', { hour: '2-digit', minute: '2-digit', hour12: false }).format(d)
  }
  if (s <= 7 * 86400) {
    return new Intl.DateTimeFormat('en-US', { weekday: 'short', hour: '2-digit', hour12: false }).format(d).replace(',', '')
  }
  return new Intl.DateTimeFormat('en-US', { month: 'short', day: 'numeric' }).format(d)
}

/** Nice tick count for a chart width in px. */
export function tickCountForWidth(width: number): number {
  if (width < 360) return 3
  if (width < 640) return 4
  if (width < 1000) return 6
  return 8
}

/** Start/end unix seconds for a range ending now. */
export function rangeWindow(key: RangeKey, now: number = Date.now()): { from: number; to: number } {
  const to = Math.floor(now / 1000)
  return { from: to - rangeSeconds(key), to }
}
