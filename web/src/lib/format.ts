// Pure formatting helpers for metrics. All functions are null-safe: undefined,
// null and NaN render as an em dash so callers never special-case missing data.

const DASH = '—'

export const EM_DASH = DASH

function isMissing(v: unknown): v is null | undefined {
  return v === null || v === undefined || (typeof v === 'number' && !Number.isFinite(v))
}

/** Trim trailing zeros from a fixed-point string ("12.50" → "12.5", "3.00" → "3"). */
function trimZeros(s: string): string {
  return s.includes('.') ? s.replace(/\.?0+$/, '') : s
}

/** Bytes as an IEC (binary) size: 1536 → "1.5 KiB". */
export function formatBytes(bytes: number | null | undefined, digits = 1): string {
  if (isMissing(bytes)) return DASH
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB']
  let v = Math.abs(bytes)
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  const sign = bytes < 0 ? '-' : ''
  const n = i === 0 ? String(Math.round(v)) : trimZeros(v.toFixed(digits))
  return `${sign}${n} ${units[i]}`
}

/** Bytes as an SI (decimal) size: 1500 → "1.5 kB". Used where vendors report decimal disks. */
export function formatBytesSI(bytes: number | null | undefined, digits = 1): string {
  if (isMissing(bytes)) return DASH
  const units = ['B', 'kB', 'MB', 'GB', 'TB', 'PB']
  let v = Math.abs(bytes)
  let i = 0
  while (v >= 1000 && i < units.length - 1) {
    v /= 1000
    i++
  }
  const sign = bytes < 0 ? '-' : ''
  const n = i === 0 ? String(Math.round(v)) : trimZeros(v.toFixed(digits))
  return `${sign}${n} ${units[i]}`
}

/** A byte rate (bytes/s) as a bitrate: 125000 → "1 Mb/s". */
export function formatBitrate(bytesPerSecond: number | null | undefined, digits = 1): string {
  if (isMissing(bytesPerSecond)) return DASH
  const bits = Math.max(0, bytesPerSecond) * 8
  const units = ['b/s', 'kb/s', 'Mb/s', 'Gb/s', 'Tb/s']
  let v = bits
  let i = 0
  while (v >= 1000 && i < units.length - 1) {
    v /= 1000
    i++
  }
  const n = i === 0 ? String(Math.round(v)) : trimZeros(v.toFixed(digits))
  return `${n} ${units[i]}`
}

/** A byte rate (bytes/s) as bytes per second: 1536 → "1.5 KiB/s". */
export function formatByteRate(bytesPerSecond: number | null | undefined, digits = 1): string {
  if (isMissing(bytesPerSecond)) return DASH
  return `${formatBytes(Math.max(0, bytesPerSecond), digits)}/s`
}

/**
 * Seconds as a compact duration: 45 → "45s", 3661 → "1h 1m", 90000 → "1d 1h".
 * `parts` limits how many units appear (default 2).
 */
export function formatDuration(seconds: number | null | undefined, parts = 2): string {
  if (isMissing(seconds)) return DASH
  let s = Math.max(0, Math.round(seconds))
  if (s < 1) return '0s'
  const units: [string, number][] = [
    ['y', 365 * 86400],
    ['d', 86400],
    ['h', 3600],
    ['m', 60],
    ['s', 1],
  ]
  const out: string[] = []
  for (const [label, size] of units) {
    if (s >= size) {
      const n = Math.floor(s / size)
      s -= n * size
      out.push(`${n}${label}`)
      if (out.length >= parts) break
    }
  }
  return out.join(' ')
}

/** Milliseconds as a duration string with sub-second precision: 850 → "850ms", 2500 → "2.5s". */
export function formatMs(ms: number | null | undefined): string {
  if (isMissing(ms)) return DASH
  if (ms < 1000) return `${Math.round(ms)}ms`
  if (ms < 60_000) return `${trimZeros((ms / 1000).toFixed(1))}s`
  return formatDuration(ms / 1000)
}

/** Network latency in ms with sensible precision: 0.42 → "0.4 ms", 12.6 → "13 ms", 250 → "250 ms". */
export function formatLatency(ms: number | null | undefined): string {
  if (isMissing(ms)) return DASH
  if (ms < 0) return DASH
  if (ms < 1) return `${trimZeros(ms.toFixed(2))} ms`
  if (ms < 10) return `${trimZeros(ms.toFixed(1))} ms`
  if (ms < 1000) return `${Math.round(ms)} ms`
  return `${trimZeros((ms / 1000).toFixed(2))} s`
}

/** A 0–100 percentage: 87.456 → "87%", with `digits` decimals if given. */
export function formatPercent(value: number | null | undefined, digits = 0): string {
  if (isMissing(value)) return DASH
  const n = digits === 0 ? String(Math.round(value)) : trimZeros(value.toFixed(digits))
  return `${n}%`
}

/** A 0–1 ratio as a percentage: 0.9987 → "99.87%". */
export function formatRatio(ratio: number | null | undefined, digits = 2): string {
  if (isMissing(ratio)) return DASH
  return formatPercent(ratio * 100, digits)
}

/** Uptime percentage keeps enough precision to distinguish 99.9 from 99.99. */
export function formatUptimePct(pct: number | null | undefined): string {
  if (isMissing(pct)) return DASH
  if (pct >= 100) return '100%'
  if (pct >= 99.9) return `${pct.toFixed(3).replace(/0+$/, '').replace(/\.$/, '')}%`
  if (pct >= 99) return `${trimZeros(pct.toFixed(2))}%`
  return `${trimZeros(pct.toFixed(1))}%`
}

/** Compact integer: 1284 → "1.3K", 4_200_000 → "4.2M". Below 1000 the number is exact. */
export function formatCompact(n: number | null | undefined): string {
  if (isMissing(n)) return DASH
  const abs = Math.abs(n)
  if (abs < 1000) return String(Math.round(n))
  const units = ['K', 'M', 'B', 'T']
  let v = abs
  let i = -1
  while (v >= 1000 && i < units.length - 1) {
    v /= 1000
    i++
  }
  const digits = v < 10 ? 1 : 0
  return `${n < 0 ? '-' : ''}${trimZeros(v.toFixed(digits))}${units[i]}`
}

/** Locale-aware integer with thousands separators. */
export function formatInt(n: number | null | undefined): string {
  if (isMissing(n)) return DASH
  return new Intl.NumberFormat('en-US', { maximumFractionDigits: 0 }).format(n)
}

/** Fixed-precision number without trailing zeros. */
export function formatNumber(n: number | null | undefined, digits = 2): string {
  if (isMissing(n)) return DASH
  return trimZeros(n.toFixed(digits))
}

/** Temperature in °C. */
export function formatTemp(c: number | null | undefined): string {
  if (isMissing(c)) return DASH
  return `${Math.round(c)}°C`
}

/** Load average with two decimals. */
export function formatLoad(v: number | null | undefined): string {
  if (isMissing(v)) return DASH
  return v.toFixed(2)
}

function toDate(input: string | number | Date | null | undefined): Date | null {
  if (isMissing(input)) return null
  if (input instanceof Date) return Number.isNaN(input.getTime()) ? null : input
  if (typeof input === 'number') {
    // unix seconds vs milliseconds heuristic
    const ms = input < 1e12 ? input * 1000 : input
    const d = new Date(ms)
    return Number.isNaN(d.getTime()) ? null : d
  }
  if (!input) return null
  const d = new Date(input)
  return Number.isNaN(d.getTime()) ? null : d
}

/**
 * Relative time with a short vocabulary: "just now", "42s ago", "5m ago",
 * "3h ago", "2d ago", "in 3d". `now` is injectable for tests.
 */
export function formatRelative(
  input: string | number | Date | null | undefined,
  now: number | Date = Date.now(),
): string {
  const d = toDate(input)
  if (!d) return DASH
  const nowMs = now instanceof Date ? now.getTime() : now
  const diff = Math.round((nowMs - d.getTime()) / 1000)
  const abs = Math.abs(diff)
  const future = diff < 0
  let text: string
  if (abs < 5) return 'just now'
  if (abs < 60) text = `${abs}s`
  else if (abs < 3600) text = `${Math.floor(abs / 60)}m`
  else if (abs < 86400) text = `${Math.floor(abs / 3600)}h`
  else if (abs < 30 * 86400) text = `${Math.floor(abs / 86400)}d`
  else if (abs < 365 * 86400) text = `${Math.floor(abs / (30 * 86400))}mo`
  else text = `${Math.floor(abs / (365 * 86400))}y`
  return future ? `in ${text}` : `${text} ago`
}

/** Absolute date-time, local zone: "Sep 28, 2026, 14:05". */
export function formatDateTime(input: string | number | Date | null | undefined): string {
  const d = toDate(input)
  if (!d) return DASH
  return new Intl.DateTimeFormat('en-US', {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  }).format(d)
}

/** Time only, local zone: "14:05" or "14:05:09" with seconds. */
export function formatTime(input: string | number | Date | null | undefined, seconds = false): string {
  const d = toDate(input)
  if (!d) return DASH
  return new Intl.DateTimeFormat('en-US', {
    hour: '2-digit',
    minute: '2-digit',
    ...(seconds ? { second: '2-digit' } : {}),
    hour12: false,
  }).format(d)
}

/** Date only, local zone: "Sep 28". Includes the year when it differs from now. */
export function formatDate(input: string | number | Date | null | undefined, now: Date = new Date()): string {
  const d = toDate(input)
  if (!d) return DASH
  const sameYear = d.getFullYear() === now.getFullYear()
  return new Intl.DateTimeFormat('en-US', {
    month: 'short',
    day: 'numeric',
    ...(sameYear ? {} : { year: 'numeric' }),
  }).format(d)
}

/** Full ISO string for tooltips/title attributes, or dash. */
export function formatISO(input: string | number | Date | null | undefined): string {
  const d = toDate(input)
  return d ? d.toISOString() : DASH
}

/** Days until an RFC3339 timestamp (negative when past). Null when missing. */
export function daysUntil(input: string | number | Date | null | undefined, now: number | Date = Date.now()): number | null {
  const d = toDate(input)
  if (!d) return null
  const nowMs = now instanceof Date ? now.getTime() : now
  return (d.getTime() - nowMs) / 86400000
}

/** Pluralize with count: plural(1, 'device') → "1 device", plural(3, 'device') → "3 devices". */
export function plural(n: number, singular: string, pluralForm = `${singular}s`): string {
  return `${formatInt(n)} ${n === 1 ? singular : pluralForm}`
}

/** Truncate the middle of a long string (node ids, keys): "nodeid-abc…xyz". */
export function truncateMiddle(s: string, max = 24): string {
  if (s.length <= max) return s
  const head = Math.ceil((max - 1) / 2)
  const tail = Math.floor((max - 1) / 2)
  return `${s.slice(0, head)}…${s.slice(s.length - tail)}`
}

/** Strip the MagicDNS suffix: "nas.tail1234.ts.net" → "nas". */
export function shortDnsName(dnsName: string | null | undefined): string {
  if (!dnsName) return DASH
  const base = dnsName.replace(/\.$/, '')
  const i = base.indexOf('.')
  return i === -1 ? base : base.slice(0, i)
}

/** Strip the "tag:" prefix for display. */
export function tagLabel(tag: string): string {
  return tag.startsWith('tag:') ? tag.slice(4) : tag
}

/** A Tailscale client version "1.72.1-t1234" → "1.72.1". */
export function shortVersion(v: string | null | undefined): string {
  if (!v) return DASH
  const m = /^v?(\d+\.\d+(?:\.\d+)?)/.exec(v)
  return m?.[1] ?? v
}
