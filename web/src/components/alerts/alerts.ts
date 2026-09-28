// Pure helpers for the Alerts page: rule-type metadata (units, labels, input
// ranges, explanations), threshold/duration formatting, scope summaries,
// alert filtering/grouping and rule-draft validation. No React; unit-tested
// in alerts.test.ts.

import type { LucideIcon } from 'lucide-react'
import {
  Activity,
  ArrowUpCircle,
  Cpu,
  HardDrive,
  KeyRound,
  MemoryStick,
  Plus,
  RadioTower,
  ShieldAlert,
  Thermometer,
  Timer,
  Waypoints,
  WifiOff,
} from 'lucide-react'
import type { Alert, AlertRule, AlertRuleType, Severity } from '../../api/types'
import { formatDate, formatDuration } from '../../lib/format'
import type { StatusTone } from '../../lib/status'

// ---------------------------------------------------------------------------
// Rule types
// ---------------------------------------------------------------------------

export type RuleUnit = 'percent' | 'ms' | 'celsius' | 'days' | 'ratio' | 'none'

export interface RuleTypeMeta {
  type: AlertRuleType
  label: string
  icon: LucideIcon
  /** Unit of `threshold` (none = the rule has no threshold). */
  unit: RuleUnit
  /** Short unit suffix for inputs and tables: "%", "ms", "°C", "days", "×". */
  unitLabel: string
  /** Field label for the threshold input, e.g. "CPU usage above". */
  thresholdLabel: string | null
  /** Field label for the duration input, e.g. "Sustained for"; null when unused. */
  forLabel: string | null
  /** Input constraints for the threshold. */
  min: number
  max: number
  step: number
  /** One-paragraph explanation shown in the rule guide and editor. */
  explanation: string
  /** Built-in escalation behaviour, if any. */
  escalation?: string
  /** Documented defaults (docs/API.md). */
  defaults: { threshold: number; forSeconds: number; severity: Severity }
  /** Whether the rule needs the per-device agent to fire. */
  needsAgent: boolean
}

export const RULE_TYPE_ORDER: readonly AlertRuleType[] = [
  'device_offline',
  'unauthorized_device',
  'new_device',
  'key_expiring',
  'update_available',
  'agent_unreachable',
  'high_cpu',
  'high_memory',
  'disk_full',
  'high_load',
  'high_temperature',
  'high_latency',
  'relay_only',
]

export const RULE_TYPES: Record<AlertRuleType, RuleTypeMeta> = {
  device_offline: {
    type: 'device_offline',
    label: 'Device offline',
    icon: WifiOff,
    unit: 'none',
    unitLabel: '',
    thresholdLabel: null,
    forLabel: 'Offline for',
    min: 0,
    max: 0,
    step: 1,
    explanation: 'Fires when a device has been offline (no tailscaled heartbeat) for longer than the grace period. Resolves as soon as the device reconnects.',
    defaults: { threshold: 0, forSeconds: 300, severity: 'warning' },
    needsAgent: false,
  },
  high_cpu: {
    type: 'high_cpu',
    label: 'High CPU',
    icon: Cpu,
    unit: 'percent',
    unitLabel: '%',
    thresholdLabel: 'CPU usage above',
    forLabel: 'Sustained for',
    min: 1,
    max: 100,
    step: 1,
    explanation: 'Fires when total CPU usage reported by the agent stays above the threshold for the whole duration. Short spikes below the duration are ignored.',
    defaults: { threshold: 90, forSeconds: 600, severity: 'warning' },
    needsAgent: true,
  },
  high_memory: {
    type: 'high_memory',
    label: 'High memory',
    icon: MemoryStick,
    unit: 'percent',
    unitLabel: '%',
    thresholdLabel: 'Memory usage above',
    forLabel: 'Sustained for',
    min: 1,
    max: 100,
    step: 1,
    explanation: 'Fires when used memory (excluding reclaimable cache) stays above the threshold for the duration.',
    defaults: { threshold: 90, forSeconds: 600, severity: 'warning' },
    needsAgent: true,
  },
  disk_full: {
    type: 'disk_full',
    label: 'Disk nearly full',
    icon: HardDrive,
    unit: 'percent',
    unitLabel: '%',
    thresholdLabel: 'Any filesystem above',
    forLabel: 'Sustained for',
    min: 1,
    max: 100,
    step: 1,
    explanation: 'Fires when any mounted filesystem reported by the agent is fuller than the threshold. Use a duration of zero to alert immediately.',
    escalation: 'Escalates to critical at 97% regardless of the configured severity.',
    defaults: { threshold: 90, forSeconds: 0, severity: 'warning' },
    needsAgent: true,
  },
  high_latency: {
    type: 'high_latency',
    label: 'High latency',
    icon: Timer,
    unit: 'ms',
    unitLabel: 'ms',
    thresholdLabel: 'Ping latency above',
    forLabel: 'Sustained for',
    min: 1,
    max: 60000,
    step: 1,
    explanation: 'Fires when the disco ping round-trip from the hub to the device stays above the threshold for the duration. Requires pings to be enabled on the hub.',
    defaults: { threshold: 250, forSeconds: 300, severity: 'info' },
    needsAgent: false,
  },
  relay_only: {
    type: 'relay_only',
    label: 'Relay only',
    icon: Waypoints,
    unit: 'none',
    unitLabel: '',
    thresholdLabel: null,
    forLabel: 'Relayed for',
    min: 0,
    max: 0,
    step: 1,
    explanation: 'Fires when a device has had no direct WireGuard path to the hub and has been going through a DERP relay for longer than the duration.',
    defaults: { threshold: 0, forSeconds: 900, severity: 'info' },
    needsAgent: false,
  },
  key_expiring: {
    type: 'key_expiring',
    label: 'Key expiring',
    icon: KeyRound,
    unit: 'days',
    unitLabel: 'days',
    thresholdLabel: 'Expires within',
    forLabel: null,
    min: 1,
    max: 365,
    step: 1,
    explanation: 'Fires when a node key expires within the threshold. Devices with key expiry disabled never trigger it.',
    escalation: 'Escalates to critical when fewer than 2 days remain.',
    defaults: { threshold: 7, forSeconds: 0, severity: 'warning' },
    needsAgent: false,
  },
  update_available: {
    type: 'update_available',
    label: 'Update available',
    icon: ArrowUpCircle,
    unit: 'none',
    unitLabel: '',
    thresholdLabel: null,
    forLabel: null,
    min: 0,
    max: 0,
    step: 1,
    explanation: 'Fires when the control API reports a newer Tailscale client for a device. Requires the control API to be configured.',
    defaults: { threshold: 0, forSeconds: 0, severity: 'info' },
    needsAgent: false,
  },
  agent_unreachable: {
    type: 'agent_unreachable',
    label: 'Agent unreachable',
    icon: RadioTower,
    unit: 'none',
    unitLabel: '',
    thresholdLabel: null,
    forLabel: 'Unreachable for',
    min: 0,
    max: 0,
    step: 1,
    explanation: 'Fires when a device that previously answered the hub stops responding on the agent port for the duration while it is still online.',
    defaults: { threshold: 0, forSeconds: 300, severity: 'warning' },
    needsAgent: true,
  },
  new_device: {
    type: 'new_device',
    label: 'New device',
    icon: Plus,
    unit: 'none',
    unitLabel: '',
    thresholdLabel: null,
    forLabel: 'Auto-resolve after',
    min: 0,
    max: 0,
    step: 1,
    explanation: 'Opens an informational alert when a device joins the tailnet and resolves it automatically after the window.',
    defaults: { threshold: 0, forSeconds: 86400, severity: 'info' },
    needsAgent: false,
  },
  unauthorized_device: {
    type: 'unauthorized_device',
    label: 'Unauthorized device',
    icon: ShieldAlert,
    unit: 'none',
    unitLabel: '',
    thresholdLabel: null,
    forLabel: null,
    min: 0,
    max: 0,
    step: 1,
    explanation: 'Fires while a device is waiting for admin approval. Resolves once it is authorized or removed. Requires the control API.',
    defaults: { threshold: 0, forSeconds: 0, severity: 'warning' },
    needsAgent: false,
  },
  high_temperature: {
    type: 'high_temperature',
    label: 'High temperature',
    icon: Thermometer,
    unit: 'celsius',
    unitLabel: '°C',
    thresholdLabel: 'Any sensor above',
    forLabel: 'Sustained for',
    min: 1,
    max: 150,
    step: 1,
    explanation: 'Fires when any temperature sensor the agent can read stays above the threshold for the duration.',
    defaults: { threshold: 85, forSeconds: 300, severity: 'warning' },
    needsAgent: true,
  },
  high_load: {
    type: 'high_load',
    label: 'High load',
    icon: Activity,
    unit: 'ratio',
    unitLabel: '×',
    thresholdLabel: 'load1 ÷ cores above',
    forLabel: 'Sustained for',
    min: 0.1,
    max: 100,
    step: 0.1,
    explanation: 'Fires when the 1-minute load average divided by the core count stays above the ratio for the duration (2.0 = twice as many runnable tasks as cores).',
    defaults: { threshold: 2, forSeconds: 600, severity: 'info' },
    needsAgent: true,
  },
}

export function ruleMeta(type: AlertRuleType): RuleTypeMeta {
  return RULE_TYPES[type] ?? RULE_TYPES.device_offline
}

export function ruleIcon(type: AlertRuleType): LucideIcon {
  return ruleMeta(type).icon
}

/** Threshold with unit: 90 → "90%", 250 → "250 ms", 85 → "85 °C", 7 → "7 days", 2 → "2.0×"; "—" when the rule has none. */
export function formatThreshold(rule: Pick<AlertRule, 'type' | 'threshold'>): string {
  const meta = ruleMeta(rule.type)
  const v = rule.threshold
  switch (meta.unit) {
    case 'percent':
      return `${trim(v)}%`
    case 'ms':
      return `${trim(v)} ms`
    case 'celsius':
      return `${trim(v)} °C`
    case 'days':
      return `${trim(v)} ${v === 1 ? 'day' : 'days'}`
    case 'ratio':
      return `${v.toFixed(1)}×`
    default:
      return '—'
  }
}

function trim(v: number): string {
  return Number.isInteger(v) ? String(v) : String(Math.round(v * 100) / 100)
}

/** Duration in seconds → "immediately" | "30s" | "5m" | "1h 30m" | "1d". */
export function formatForSeconds(seconds: number | null | undefined): string {
  if (seconds === null || seconds === undefined || !Number.isFinite(seconds) || seconds <= 0) return 'immediately'
  return formatDuration(seconds, 2)
}

/** Split a duration into whole minutes + remaining seconds for the editor's paired inputs. */
export function splitSeconds(total: number): { minutes: number; seconds: number } {
  const t = Math.max(0, Math.round(total || 0))
  return { minutes: Math.floor(t / 60), seconds: t % 60 }
}

/** Inverse of splitSeconds; non-finite/negative parts count as zero. */
export function joinSeconds(minutes: number, seconds: number): number {
  const m = Number.isFinite(minutes) && minutes > 0 ? Math.floor(minutes) : 0
  const s = Number.isFinite(seconds) && seconds > 0 ? Math.floor(seconds) : 0
  return m * 60 + s
}

/** "All devices" or a compact description of include/exclude lists. */
export function ruleScopeSummary(rule: Pick<AlertRule, 'includeTags' | 'excludeTags' | 'includeDevices' | 'excludeDevices'>, deviceName?: (id: string) => string | undefined): string {
  const inc = [...(rule.includeTags ?? []), ...(rule.includeDevices ?? []).map((id) => deviceName?.(id) ?? id)]
  const exc = [...(rule.excludeTags ?? []), ...(rule.excludeDevices ?? []).map((id) => deviceName?.(id) ?? id)]
  const list = (xs: string[]) => (xs.length <= 2 ? xs.join(', ') : `${xs.slice(0, 2).join(', ')} +${xs.length - 2}`)
  if (!inc.length && !exc.length) return 'All devices'
  const parts: string[] = []
  if (inc.length) parts.push(`Only ${list(inc)}`)
  if (exc.length) parts.push(`Except ${list(exc)}`)
  return parts.join(' · ')
}

export function severityOptions(): { value: Severity; label: string }[] {
  return [
    { value: 'info', label: 'Info' },
    { value: 'warning', label: 'Warning' },
    { value: 'critical', label: 'Critical' },
  ]
}

// ---------------------------------------------------------------------------
// Rule drafts (editor state)
// ---------------------------------------------------------------------------

export interface RuleDraft {
  name: string
  description: string
  enabled: boolean
  severity: Severity
  /** Raw threshold text so partially typed numbers survive re-renders. */
  threshold: string
  minutes: string
  seconds: string
  notify: boolean
  includeTags: string[]
  excludeTags: string[]
  includeDevices: string[]
  excludeDevices: string[]
}

export function draftFromRule(rule: AlertRule): RuleDraft {
  const { minutes, seconds } = splitSeconds(rule.forSeconds)
  return {
    name: rule.name,
    description: rule.description ?? '',
    enabled: rule.enabled,
    severity: rule.severity,
    threshold: String(rule.threshold),
    minutes: String(minutes),
    seconds: String(seconds),
    notify: rule.notify,
    includeTags: [...(rule.includeTags ?? [])],
    excludeTags: [...(rule.excludeTags ?? [])],
    includeDevices: [...(rule.includeDevices ?? [])],
    excludeDevices: [...(rule.excludeDevices ?? [])],
  }
}

export type RuleDraftErrors = Partial<Record<'name' | 'threshold' | 'duration' | 'scope', string>>

/** Validate a draft against its rule type; returns an empty object when valid. */
export function validateRuleDraft(draft: RuleDraft, type: AlertRuleType): RuleDraftErrors {
  const meta = ruleMeta(type)
  const errors: RuleDraftErrors = {}
  const name = draft.name.trim()
  if (!name) errors.name = 'Enter a name'
  else if (name.length > 80) errors.name = 'Names are limited to 80 characters'

  if (meta.unit !== 'none') {
    const t = Number(draft.threshold)
    if (draft.threshold.trim() === '' || !Number.isFinite(t)) errors.threshold = 'Enter a number'
    else if (t < meta.min) errors.threshold = `Must be at least ${meta.min}${meta.unitLabel === '%' ? '%' : ` ${meta.unitLabel}`.trimEnd()}`
    else if (t > meta.max) errors.threshold = `Must be at most ${meta.max}${meta.unitLabel === '%' ? '%' : ` ${meta.unitLabel}`.trimEnd()}`
  }

  if (meta.forLabel) {
    const m = Number(draft.minutes)
    const s = Number(draft.seconds)
    if (draft.minutes.trim() === '' || !Number.isFinite(m) || m < 0 || !Number.isInteger(m)) errors.duration = 'Minutes must be a whole number'
    else if (draft.seconds.trim() === '' || !Number.isFinite(s) || s < 0 || s > 59 || !Number.isInteger(s)) errors.duration = 'Seconds must be between 0 and 59'
    else if (joinSeconds(m, s) > 30 * 86400) errors.duration = 'Durations are limited to 30 days'
  }

  const overlapTags = draft.includeTags.filter((t) => draft.excludeTags.includes(t))
  const overlapDevices = draft.includeDevices.filter((d) => draft.excludeDevices.includes(d))
  if (overlapTags.length) errors.scope = `${overlapTags[0]} is both included and excluded`
  else if (overlapDevices.length) errors.scope = 'A device is both included and excluded'
  return errors
}

/** Build the rule to PUT from a validated draft. Unchanged identity fields come from `base`. */
export function ruleFromDraft(base: AlertRule, draft: RuleDraft): AlertRule {
  const meta = ruleMeta(base.type)
  const threshold = meta.unit === 'none' ? 0 : Number(draft.threshold)
  const forSeconds = meta.forLabel ? joinSeconds(Number(draft.minutes), Number(draft.seconds)) : 0
  const clean = (xs: string[]) => Array.from(new Set(xs.map((x) => x.trim()).filter(Boolean)))
  return {
    ...base,
    name: draft.name.trim(),
    description: draft.description.trim() || undefined,
    enabled: draft.enabled,
    severity: draft.severity,
    threshold: Number.isFinite(threshold) ? threshold : base.threshold,
    forSeconds,
    notify: draft.notify,
    includeTags: clean(draft.includeTags),
    excludeTags: clean(draft.excludeTags),
    includeDevices: clean(draft.includeDevices),
    excludeDevices: clean(draft.excludeDevices),
  }
}

/** Tailscale ACL tag: `tag:` + [a-z0-9-]. */
export const TAG_RE = /^tag:[a-z0-9-]+$/

/** Normalise user input to a tag ("Server" → "tag:server"); empty string when blank. */
export function normalizeTag(input: string): string {
  const s = input.trim().toLowerCase()
  if (!s) return ''
  return s.startsWith('tag:') ? s : `tag:${s}`
}

/** Error for a candidate tag, or null when acceptable. */
export function validateTag(tag: string, existing: ReadonlyArray<string> = []): string | null {
  if (!tag) return 'Enter a tag name'
  if (!TAG_RE.test(tag)) return 'Tags look like tag:name (lowercase letters, digits and dashes)'
  if (existing.includes(tag)) return 'Already in the list'
  return null
}

// ---------------------------------------------------------------------------
// Alert lists
// ---------------------------------------------------------------------------

export type SeverityFilter = 'all' | Severity

export interface AlertFilters {
  severity: SeverityFilter
  /** Device id ('' = any). */
  device: string
  q: string
}

export const DEFAULT_ALERT_FILTERS: AlertFilters = { severity: 'all', device: '', q: '' }

export function hasAlertFilters(f: AlertFilters): boolean {
  return f.severity !== 'all' || f.device !== '' || f.q.trim() !== ''
}

/**
 * Filters seeded from a deep link (`?severity=critical` from the overview,
 * `?device=<id>` from a device's "Alert history"). Params that are absent or
 * invalid leave `base` untouched, so this never clears a user's own filter.
 */
export function alertFiltersFromSearch(sp: URLSearchParams, base: AlertFilters = DEFAULT_ALERT_FILTERS): AlertFilters {
  const sev = sp.get('severity')
  const device = sp.get('device')
  return {
    ...base,
    severity: sev === 'critical' || sev === 'warning' || sev === 'info' ? sev : base.severity,
    device: device ?? base.device,
  }
}

/** Case-insensitive match on title, message, device name and rule type. */
export function filterAlerts(alerts: ReadonlyArray<Alert>, f: AlertFilters): Alert[] {
  const q = f.q.trim().toLowerCase()
  return alerts.filter((a) => {
    if (f.severity !== 'all' && a.severity !== f.severity) return false
    if (f.device && a.deviceId !== f.device) return false
    if (q) {
      const hay = `${a.title} ${a.message} ${a.deviceName ?? ''} ${a.ruleType} ${a.ackedBy ?? ''}`.toLowerCase()
      if (!hay.includes(q)) return false
    }
    return true
  })
}

const SEVERITY_ORDER: Record<Severity, number> = { critical: 0, warning: 1, info: 2 }

export interface AlertGroup {
  key: string
  label: string
  tone: StatusTone
  alerts: Alert[]
}

/** Open alerts grouped by severity (critical → info); unacknowledged first, then newest, inside each group. */
export function groupOpenAlerts(alerts: ReadonlyArray<Alert>): AlertGroup[] {
  const sorted = [...alerts].sort((a, b) => {
    const sev = SEVERITY_ORDER[a.severity] - SEVERITY_ORDER[b.severity]
    if (sev) return sev
    const ack = Number(!!a.ackedAt) - Number(!!b.ackedAt)
    if (ack) return ack
    return b.openedAt.localeCompare(a.openedAt) || b.id - a.id
  })
  const groups: AlertGroup[] = []
  for (const a of sorted) {
    let g = groups.find((x) => x.key === a.severity)
    if (!g) {
      g = { key: a.severity, label: a.severity === 'critical' ? 'Critical' : a.severity === 'warning' ? 'Warning' : 'Info', tone: a.severity === 'critical' ? 'critical' : a.severity === 'warning' ? 'warning' : 'info', alerts: [] }
      groups.push(g)
    }
    g.alerts.push(a)
  }
  return groups
}

/** Local calendar day key "YYYY-MM-DD". */
export function dayKey(input: string | number | Date): string {
  const d = new Date(input)
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${day}`
}

/** "Today", "Yesterday", "Mon, Sep 22" (same year) or "Sep 22, 2025". */
export function dayLabel(input: string | number | Date, now: Date = new Date()): string {
  const d = new Date(input)
  const key = dayKey(d)
  if (key === dayKey(now)) return 'Today'
  const yesterday = new Date(now)
  yesterday.setDate(now.getDate() - 1)
  if (key === dayKey(yesterday)) return 'Yesterday'
  if (d.getFullYear() === now.getFullYear()) {
    return new Intl.DateTimeFormat('en-US', { weekday: 'short', month: 'short', day: 'numeric' }).format(d)
  }
  return formatDate(d, now)
}

/** Resolved alerts grouped by the day they resolved (newest day first, newest alert first). */
export function groupResolvedAlerts(alerts: ReadonlyArray<Alert>, now: Date = new Date()): AlertGroup[] {
  const sorted = [...alerts].sort((a, b) => (b.resolvedAt ?? b.updatedAt).localeCompare(a.resolvedAt ?? a.updatedAt) || b.id - a.id)
  const groups: AlertGroup[] = []
  for (const a of sorted) {
    const ts = a.resolvedAt ?? a.updatedAt
    const key = dayKey(ts)
    let g = groups.find((x) => x.key === key)
    if (!g) {
      g = { key, label: dayLabel(ts, now), tone: 'neutral', alerts: [] }
      groups.push(g)
    }
    g.alerts.push(a)
  }
  return groups
}

/** Distinct devices referenced by a list of alerts, sorted by name. */
export function alertDeviceOptions(alerts: ReadonlyArray<Alert>): { value: string; label: string }[] {
  const map = new Map<string, string>()
  for (const a of alerts) if (a.deviceId) map.set(a.deviceId, a.deviceName ?? a.deviceId)
  return Array.from(map, ([value, label]) => ({ value, label })).sort((a, b) => a.label.localeCompare(b.label))
}

/** How long an alert has been (or was) open, in seconds. */
export function alertDurationSeconds(a: Pick<Alert, 'openedAt' | 'resolvedAt'>, now: number = Date.now()): number {
  const start = new Date(a.openedAt).getTime()
  const end = a.resolvedAt ? new Date(a.resolvedAt).getTime() : now
  return Math.max(0, Math.round((end - start) / 1000))
}

/** Format an alert's measured value using its rule's unit ("92.3%", "312 ms", "3 days"). */
export function formatAlertValue(a: Pick<Alert, 'ruleType' | 'value'>): string | null {
  if (a.value === null || a.value === undefined || !Number.isFinite(a.value)) return null
  const meta = ruleMeta(a.ruleType)
  const v = a.value
  switch (meta.unit) {
    case 'percent':
      return `${Math.round(v * 10) / 10}%`
    case 'ms':
      return `${Math.round(v)} ms`
    case 'celsius':
      return `${Math.round(v)} °C`
    case 'days':
      return `${Math.round(v)} ${Math.round(v) === 1 ? 'day' : 'days'}`
    case 'ratio':
      return `${v.toFixed(2)}×`
    default:
      return null
  }
}
