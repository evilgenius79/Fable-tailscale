import type { AgentState, Alert, AlertState, Device, EventType, PathType, Severity } from '@/api/types'

/** The visual status vocabulary used by StatusDot / Badge / StatTile tones. */
export type StatusTone = 'online' | 'offline' | 'warning' | 'critical' | 'relay' | 'direct' | 'info' | 'neutral' | 'accent'

export interface DeviceStatus {
  tone: StatusTone
  /** Short label suitable for badges: "Online", "Offline", "Unauthorized", … */
  label: string
  /** One-line explanation for tooltips. */
  detail: string
}

/**
 * Collapse a device into one headline status. Precedence: unauthorized/expired
 * (critical) → offline → agent problems / disk / memory / key expiry (warning)
 * → relay (info) → online.
 */
export function deviceStatus(d: Device): DeviceStatus {
  if (!d.authorized) return { tone: 'critical', label: 'Unauthorized', detail: 'Device is waiting for authorization' }
  if (d.expired) return { tone: 'critical', label: 'Key expired', detail: 'Node key has expired' }
  if (!d.online) return { tone: 'offline', label: 'Offline', detail: 'Device is not connected to the tailnet' }
  const issues = deviceIssues(d)
  const critical = issues.find((i) => i.severity === 'critical')
  if (critical) return { tone: 'critical', label: critical.label, detail: critical.detail }
  const warn = issues.find((i) => i.severity === 'warning')
  if (warn) return { tone: 'warning', label: warn.label, detail: warn.detail }
  return { tone: 'online', label: 'Online', detail: d.connectivity.path === 'relay' ? 'Online via DERP relay' : 'Online' }
}

export interface DeviceIssue {
  id: string
  severity: Severity
  label: string
  detail: string
}

/** Every noteworthy condition on a device, most severe first. */
export function deviceIssues(d: Device): DeviceIssue[] {
  const out: DeviceIssue[] = []
  const m = d.metrics
  if (!d.authorized) out.push({ id: 'unauthorized', severity: 'warning', label: 'Unauthorized', detail: 'Waiting for admin approval' })
  if (d.expired) out.push({ id: 'expired', severity: 'critical', label: 'Key expired', detail: 'Node key has expired' })
  if (d.keyExpiry && !d.keyExpiryDisabled && !d.expired) {
    const days = (new Date(d.keyExpiry).getTime() - Date.now()) / 86400000
    if (days < 2) out.push({ id: 'key', severity: 'critical', label: 'Key expiring', detail: `Node key expires in ${Math.max(0, Math.round(days * 24))}h` })
    else if (days < 7) out.push({ id: 'key', severity: 'warning', label: 'Key expiring', detail: `Node key expires in ${Math.round(days)}d` })
  }
  if (m) {
    if (m.diskPercent >= 97) out.push({ id: 'disk', severity: 'critical', label: 'Disk full', detail: `Disk ${Math.round(m.diskPercent)}% used` })
    else if (m.diskPercent >= 90) out.push({ id: 'disk', severity: 'warning', label: 'Disk nearly full', detail: `Disk ${Math.round(m.diskPercent)}% used` })
    if (m.memPercent >= 90) out.push({ id: 'mem', severity: 'warning', label: 'High memory', detail: `Memory ${Math.round(m.memPercent)}% used` })
    if (m.cpuPercent >= 90) out.push({ id: 'cpu', severity: 'warning', label: 'High CPU', detail: `CPU ${Math.round(m.cpuPercent)}%` })
    const hot = (m.temperatures ?? []).find((t) => t.celsius >= 85)
    if (hot) out.push({ id: 'temp', severity: 'warning', label: 'Running hot', detail: `${hot.sensor} ${Math.round(hot.celsius)}°C` })
  }
  if (d.online && d.agent.state === 'unreachable') out.push({ id: 'agent', severity: 'warning', label: 'Agent unreachable', detail: d.agent.lastError ?? 'Agent did not respond' })
  if (d.updateAvailable) out.push({ id: 'update', severity: 'info', label: 'Update available', detail: `Client ${d.clientVersion ?? ''} can be updated`.trim() })
  if (d.online && d.connectivity.path === 'relay') out.push({ id: 'relay', severity: 'info', label: 'Relayed', detail: `Traffic via DERP ${d.connectivity.relay ?? ''}`.trim() })
  const order: Record<Severity, number> = { critical: 0, warning: 1, info: 2 }
  return out.sort((a, b) => order[a.severity] - order[b.severity])
}

export function severityTone(s: Severity): StatusTone {
  return s === 'critical' ? 'critical' : s === 'warning' ? 'warning' : 'info'
}

export function severityLabel(s: Severity): string {
  return s === 'critical' ? 'Critical' : s === 'warning' ? 'Warning' : 'Info'
}

export function pathTone(p: PathType): StatusTone {
  return p === 'direct' ? 'direct' : p === 'relay' ? 'relay' : p === 'none' ? 'offline' : 'neutral'
}

export function pathLabel(p: PathType, relay?: string): string {
  if (p === 'direct') return 'Direct'
  if (p === 'relay') return relay ? `Relay · ${relay}` : 'Relay'
  if (p === 'none') return 'No path'
  return 'Unknown'
}

export function agentTone(s: AgentState): StatusTone {
  return s === 'reachable' ? 'online' : s === 'unreachable' ? 'warning' : s === 'disabled' ? 'neutral' : 'offline'
}

export function agentLabel(s: AgentState): string {
  return s === 'reachable' ? 'Agent online' : s === 'unreachable' ? 'Agent unreachable' : s === 'disabled' ? 'Agent disabled' : 'No agent'
}

export function alertTone(a: Pick<Alert, 'state' | 'severity'>): StatusTone {
  return a.state === 'resolved' ? 'online' : severityTone(a.severity)
}

export function alertStateLabel(s: AlertState): string {
  return s === 'open' ? 'Open' : 'Resolved'
}

/** Tone for an event type/severity pair (used by event feeds). */
export function eventTone(type: EventType, severity: Severity): StatusTone {
  switch (type) {
    case 'device.online':
    case 'agent.reachable':
    case 'alert.resolved':
    case 'device.authorized':
      return 'online'
    case 'device.offline':
    case 'device.removed':
    case 'device.expired':
      return 'offline'
    case 'device.path_changed':
      return 'relay'
    case 'admin.action':
      return 'accent'
    default:
      return severityTone(severity)
  }
}

/** Human label for an event type: "device.path_changed" → "Path changed". */
export function eventTypeLabel(type: EventType): string {
  const map: Record<EventType, string> = {
    'device.online': 'Device online',
    'device.offline': 'Device offline',
    'device.new': 'New device',
    'device.removed': 'Device removed',
    'device.updated': 'Device updated',
    'device.authorized': 'Device authorized',
    'device.expired': 'Key expired',
    'device.path_changed': 'Path changed',
    'agent.reachable': 'Agent reachable',
    'agent.unreachable': 'Agent unreachable',
    'alert.opened': 'Alert opened',
    'alert.resolved': 'Alert resolved',
    'alert.acked': 'Alert acknowledged',
    'admin.action': 'Admin action',
    'hub.started': 'Hub started',
    'hub.error': 'Hub error',
  }
  return map[type] ?? type
}

/** Threshold tone for a 0–100 utilisation value. */
export function utilizationTone(pct: number | null | undefined, warn = 75, crit = 90): StatusTone {
  if (pct === null || pct === undefined || !Number.isFinite(pct)) return 'neutral'
  if (pct >= crit) return 'critical'
  if (pct >= warn) return 'warning'
  return 'accent'
}
