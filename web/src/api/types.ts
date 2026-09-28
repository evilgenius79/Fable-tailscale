// TypeScript mirror of internal/model/model.go JSON shapes (docs/API.md).
// Keep field names identical to the Go json tags.

export type DeviceID = string
export type PathType = 'direct' | 'relay' | 'none' | 'unknown'
export type AgentState = 'reachable' | 'unreachable' | 'unknown' | 'disabled'
export type Severity = 'info' | 'warning' | 'critical'
export type Role = 'viewer' | 'admin'
export type AlertState = 'open' | 'resolved'

export interface Location {
  country?: string
  countryCode?: string
  city?: string
  latitude?: number
  longitude?: number
}

export interface NATSupport {
  hairPinning: boolean
  ipv6: boolean
  pcp: boolean
  pmp: boolean
  udp: boolean
  upnp: boolean
}

export interface Connectivity {
  path: PathType
  relay?: string
  curAddr?: string
  endpoints?: string[]
  latencyMs?: number
  lastPing?: string
  rxBytes: number
  txBytes: number
  rxRate: number
  txRate: number
  derpLatencyMs?: Record<string, number>
  preferredDerp?: string
  natSupport?: NATSupport
  mappingVariesByDestIp?: boolean
}

export interface AgentStatus {
  state: AgentState
  version?: string
  lastSuccess?: string
  lastError?: string
  url?: string
}

export interface DiskUsage {
  mount: string
  fstype?: string
  total: number
  used: number
  percent: number
}

export interface InterfaceCounters {
  name: string
  rxBytes: number
  txBytes: number
  rxPackets: number
  txPackets: number
  rxErrors: number
  txErrors: number
  rxRate: number
  txRate: number
}

export interface Temperature {
  sensor: string
  celsius: number
  critical?: number
}

export interface MetricsSnapshot {
  sampledAt: string
  platform?: string
  platformVersion?: string
  kernel?: string
  arch?: string
  bootTime?: string
  uptimeSeconds: number
  cpuPercent: number
  cpuCount: number
  cpuModel?: string
  perCore?: number[]
  load1: number
  load5: number
  load15: number
  memTotal: number
  memUsed: number
  memAvailable: number
  memPercent: number
  swapTotal: number
  swapUsed: number
  disks: DiskUsage[]
  diskPercent: number
  interfaces: InterfaceCounters[]
  tailscaleInterface?: string
  netRxRate: number
  netTxRate: number
  temperatures?: Temperature[]
  processes: number
  tailscaleVersion?: string
}

export interface UptimeStats {
  pct24h?: number
  pct7d?: number
  pct30d?: number
  lastChange?: string
  onlineForSeconds?: number
}

export interface Device {
  id: DeviceID
  name: string
  dnsName: string
  hostname: string
  os: string
  deviceModel?: string
  addresses: string[]
  user: string
  userDisplayName?: string
  tags: string[]
  isSelf: boolean
  isExternal: boolean
  online: boolean
  active: boolean
  lastSeen: string
  lastHandshake?: string
  created: string
  firstSeen: string
  updatedAt: string
  clientVersion?: string
  updateAvailable: boolean
  authorized: boolean
  keyExpiry?: string
  keyExpiryDisabled: boolean
  expired: boolean
  exitNodeOption: boolean
  isExitNode: boolean
  advertisedRoutes: string[]
  enabledRoutes: string[]
  primaryRoutes: string[]
  blocksIncomingConnections: boolean
  location?: Location
  connectivity: Connectivity
  agent: AgentStatus
  metrics?: MetricsSnapshot
  uptime: UptimeStats
}

export interface SeriesPoint {
  t: number
  online: number
  direct?: number
  latencyMs?: number
  latencyMax?: number
  tsRxRate: number
  tsTxRate: number
  cpu?: number
  cpuMax?: number
  mem?: number
  disk?: number
  load1?: number
  netRxRate?: number
  netTxRate?: number
  tempC?: number
}

export interface Series {
  deviceId: DeviceID
  from: string
  to: string
  stepSeconds: number
  source: 'raw' | 'rollup'
  points: SeriesPoint[]
}

export interface TimelineSegment {
  from: string
  to: string
  online: boolean
}

export interface UptimeReport {
  deviceId: DeviceID
  from: string
  to: string
  pct: number
  segments: TimelineSegment[]
  outages: number
}

export type EventType =
  | 'device.online'
  | 'device.offline'
  | 'device.new'
  | 'device.removed'
  | 'device.updated'
  | 'device.authorized'
  | 'device.expired'
  | 'device.path_changed'
  | 'agent.reachable'
  | 'agent.unreachable'
  | 'alert.opened'
  | 'alert.resolved'
  | 'alert.acked'
  | 'admin.action'
  | 'hub.started'
  | 'hub.error'

export interface Event {
  id: number
  ts: string
  type: EventType
  severity: Severity
  deviceId?: DeviceID
  deviceName?: string
  title: string
  message?: string
  data?: Record<string, unknown>
}

export type AlertRuleType =
  | 'device_offline'
  | 'high_cpu'
  | 'high_memory'
  | 'disk_full'
  | 'high_latency'
  | 'relay_only'
  | 'key_expiring'
  | 'update_available'
  | 'agent_unreachable'
  | 'new_device'
  | 'unauthorized_device'
  | 'high_temperature'
  | 'high_load'

export interface AlertRule {
  id: string
  type: AlertRuleType
  name: string
  description?: string
  enabled: boolean
  severity: Severity
  threshold: number
  forSeconds: number
  includeTags?: string[]
  excludeTags?: string[]
  includeDevices?: DeviceID[]
  excludeDevices?: DeviceID[]
  notify: boolean
  updatedAt: string
}

export interface Alert {
  id: number
  ruleId: string
  ruleType: AlertRuleType
  deviceId?: DeviceID
  deviceName?: string
  state: AlertState
  severity: Severity
  title: string
  message: string
  value?: number
  openedAt: string
  updatedAt: string
  resolvedAt?: string
  ackedAt?: string
  ackedBy?: string
  data?: Record<string, unknown>
}

export interface AuditEntry {
  id: number
  ts: string
  actor: string
  actorNode: string
  action: string
  target: string
  details?: Record<string, unknown>
  ok: boolean
  error?: string
  remoteIp?: string
}

export interface Identity {
  login: string
  displayName: string
  profilePicUrl?: string
  nodeName: string
  nodeId: DeviceID
  nodeIp: string
  tags?: string[]
  role: Role
  authMode: 'tailscale' | 'none'
}

export interface HubInfo {
  version: string
  tailnet: string
  magicDnsSuffix: string
  selfName: string
  selfId: DeviceID
  selfIps: string[]
  tailscaleVersion: string
  backendState: string
  health: string[]
  startedAt: string
  demoMode: boolean
  controlApiEnabled: boolean
  adminActionsEnabled: boolean
  agentPort: number
  pollIntervalSeconds: number
  lastPoll: string
  lastApiPoll?: string
  lastError?: string
}

export interface OverviewSparklines {
  t: number[]
  online: number[]
  rxRate: number[]
  txRate: number[]
  latency: number[]
}

export interface Overview {
  hub: HubInfo
  devices: number
  online: number
  offline: number
  direct: number
  relayed: number
  agentsReachable: number
  updatesPending: number
  keysExpiringSoon: number
  unauthorized: number
  exitNodes: number
  subnetRouters: number
  openAlerts: number
  criticalAlerts: number
  totalRxRate: number
  totalTxRate: number
  totalRxBytes: number
  totalTxBytes: number
  avgLatencyMs?: number
  osBreakdown: Record<string, number>
  userBreakdown: Record<string, number>
  sparklines: OverviewSparklines
}

export interface TopologyNode {
  id: DeviceID
  name: string
  os: string
  online: boolean
  isSelf: boolean
  isExitNode: boolean
  isSubnetRouter: boolean
  tags: string[]
  user: string
  relay?: string
  path: PathType
  latencyMs?: number
}

export interface TopologyEdge {
  from: DeviceID
  to: DeviceID
  path: PathType
  relay?: string
  latencyMs?: number
  rxRate: number
  txRate: number
}

export interface Topology {
  nodes: TopologyNode[]
  edges: TopologyEdge[]
  derpRegions: Record<string, string>
}

export interface PingResult {
  deviceId: DeviceID
  ip: string
  latencyMs: number
  path: PathType
  endpoint?: string
  relay?: string
  error?: string
  at: string
}

export interface StoreStats {
  devices: number
  samples: number
  rollups: number
  events: number
  alerts: number
  sizeBytes: number
  oldestSample?: string
}

export interface Settings {
  authMode: string
  adminUsers: string[]
  adminTags: string[]
  viewerUsers: string[]
  viewerTags: string[]
  adminActionsEnabled: boolean
  controlApiEnabled: boolean
  tailnet?: string
  pollIntervalSeconds: number
  apiIntervalSeconds: number
  pingIntervalSeconds: number
  agentPort: number
  agentEnabled: boolean
  rawRetentionHours: number
  rollupRetentionDays: number
  eventRetentionDays: number
  notifiers: string[]
  listen: string
  demoMode: boolean
  store: StoreStats
}

export interface DeviceDetail {
  device: Device
  recentEvents: Event[]
  openAlerts: Alert[]
  uptime24h?: UptimeReport
}

export interface APIError {
  error: { code: string; message: string }
}

export type SSEHello = { identity: Identity; hub: HubInfo }
export type SSETick = { overview: Overview; devices: Device[] }
