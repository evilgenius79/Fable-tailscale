// Deterministic demo fixtures for VITE_MOCK=1. Mirrors the fleet described in
// docs/ARCHITECTURE.md (internal/demo). Everything here is plain data + pure
// functions; mock.ts wires it to fetch/EventSource. Never imported in prod.

import type {
  Alert,
  AlertRule,
  AlertRuleType,
  AuditEntry,
  Device,
  DeviceDetail,
  Event,
  EventType,
  HubInfo,
  Identity,
  MetricsSnapshot,
  Overview,
  PathType,
  Series,
  SeriesPoint,
  Settings,
  Severity,
  TimelineSegment,
  Topology,
  TopologyEdge,
  TopologyNode,
  UptimeReport,
} from './types'
import { rangeSeconds, rangeStepSeconds, type RangeKey } from '../lib/time'

// ---------------------------------------------------------------------------
// PRNG helpers (deterministic, seedable)
// ---------------------------------------------------------------------------

export function hashString(s: string): number {
  let h = 2166136261
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i)
    h = Math.imul(h, 16777619)
  }
  return h >>> 0
}

export function mulberry32(seed: number): () => number {
  let a = seed >>> 0
  return () => {
    a = (a + 0x6d2b79f5) >>> 0
    let t = a
    t = Math.imul(t ^ (t >>> 15), t | 1)
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61)
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

/** Stable pseudo-noise in [-1, 1] for (seed, t). */
function noise(seed: number, t: number): number {
  const r = mulberry32((seed ^ Math.imul(Math.floor(t), 2654435761)) >>> 0)
  return r() * 2 - 1
}

const clamp = (v: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, v))
const iso = (ms: number) => new Date(ms).toISOString()

// ---------------------------------------------------------------------------
// Fleet profiles
// ---------------------------------------------------------------------------

export const MAGIC_DNS_SUFFIX = 'tail4c2f.ts.net'
export const TAILNET = 'example.com'

interface Profile {
  id: string
  name: string
  os: string
  model?: string
  user: string
  userDisplayName?: string
  tags?: string[]
  ip4: string
  ip6: string
  isSelf?: boolean
  isExternal?: boolean
  online: boolean
  /** minutes ago the device was last seen (only when offline). */
  offlineForMin?: number
  path: PathType
  relay?: string
  latency?: number
  agent: 'reachable' | 'unreachable' | 'unknown' | 'disabled'
  agentError?: string
  clientVersion: string
  updateAvailable?: boolean
  authorized?: boolean
  keyExpiryDays?: number
  keyExpiryDisabled?: boolean
  exitNode?: boolean
  advertisedRoutes?: string[]
  enabledRoutes?: string[]
  createdDaysAgo: number
  flapper?: boolean
  // metric baselines
  cpu?: number
  cpuAmp?: number
  mem?: number
  disk?: number
  diskTotal?: number
  memTotal?: number
  cpuCount?: number
  cpuModel?: string
  load?: number
  rx?: number
  tx?: number
  temp?: number
  platform?: string
  platformVersion?: string
  kernel?: string
  arch?: string
  uptimeDays?: number
  location?: { city: string; country: string; countryCode: string; latitude: number; longitude: number }
}

export const PROFILES: Profile[] = [
  {
    id: 'nQ7f3A1hub', name: 'netwatch-hub', os: 'linux', model: 'Intel NUC 11', user: 'alice@example.com', userDisplayName: 'Alice Park',
    ip4: '100.64.0.1', ip6: 'fd7a:115c:a1e0::1', isSelf: true, online: true, path: 'direct', latency: 0.4, agent: 'reachable',
    clientVersion: '1.82.0', createdDaysAgo: 410, cpu: 12, cpuAmp: 10, mem: 41, disk: 38, diskTotal: 512e9, memTotal: 16 * 2 ** 30, cpuCount: 4,
    cpuModel: 'Intel Core i5-1135G7', load: 0.4, rx: 180e3, tx: 95e3, platform: 'ubuntu', platformVersion: '24.04', kernel: '6.8.0-45-generic', arch: 'amd64', uptimeDays: 61,
    location: { city: 'Portland', country: 'United States', countryCode: 'US', latitude: 45.52, longitude: -122.68 },
  },
  {
    id: 'nB2c9Dnas0', name: 'nas-basement', os: 'linux', model: 'Synology DS923+', user: 'alice@example.com', userDisplayName: 'Alice Park', tags: ['tag:nas'],
    ip4: '100.64.0.2', ip6: 'fd7a:115c:a1e0::2', online: true, path: 'direct', latency: 1.2, agent: 'reachable', clientVersion: '1.82.0',
    advertisedRoutes: ['192.168.1.0/24'], enabledRoutes: ['192.168.1.0/24'], createdDaysAgo: 380, cpu: 9, cpuAmp: 14, mem: 55, disk: 91,
    diskTotal: 24e12, memTotal: 8 * 2 ** 30, cpuCount: 4, cpuModel: 'AMD Ryzen R1600', load: 0.7, rx: 2.4e6, tx: 850e3, platform: 'synology', platformVersion: 'DSM 7.2', kernel: '4.4.302+', arch: 'amd64', uptimeDays: 143,
  },
  {
    id: 'nP4i8Ggar1', name: 'pi-garage', os: 'linux', model: 'Raspberry Pi 4 Model B', user: 'alice@example.com', userDisplayName: 'Alice Park', tags: ['tag:iot'],
    ip4: '100.64.0.3', ip6: 'fd7a:115c:a1e0::3', online: true, path: 'relay', relay: 'nyc', latency: 34, agent: 'reachable', clientVersion: '1.82.0',
    createdDaysAgo: 220, cpu: 18, cpuAmp: 8, mem: 47, disk: 71, diskTotal: 32e9, memTotal: 4 * 2 ** 30, cpuCount: 4, cpuModel: 'Cortex-A72', load: 0.5, rx: 42e3, tx: 18e3, temp: 71,
    platform: 'raspbian', platformVersion: '12', kernel: '6.6.31+rpt-rpi-v8', arch: 'arm64', uptimeDays: 19,
  },
  {
    id: 'nA1l2Mmbp2', name: 'alice-mbp', os: 'macOS', model: 'MacBook Pro 14" M3', user: 'alice@example.com', userDisplayName: 'Alice Park',
    ip4: '100.64.0.4', ip6: 'fd7a:115c:a1e0::4', online: true, path: 'direct', latency: 2.1, agent: 'reachable', clientVersion: '1.80.3', updateAvailable: true,
    createdDaysAgo: 300, cpu: 23, cpuAmp: 25, mem: 68, disk: 54, diskTotal: 1e12, memTotal: 36 * 2 ** 30, cpuCount: 12, cpuModel: 'Apple M3 Pro', load: 2.1, rx: 620e3, tx: 210e3, platform: 'darwin', platformVersion: '15.1', kernel: '24.1.0', arch: 'arm64', uptimeDays: 6,
  },
  {
    id: 'nB0b1Ttpd3', name: 'bob-thinkpad', os: 'windows', model: 'ThinkPad X1 Carbon Gen 11', user: 'bob@example.com', userDisplayName: 'Bob Reyes',
    ip4: '100.64.0.5', ip6: 'fd7a:115c:a1e0::5', online: true, path: 'direct', latency: 3.4, agent: 'unreachable', agentError: 'dial tcp 100.64.0.5:41820: connection refused',
    clientVersion: '1.82.0', createdDaysAgo: 250, cpu: 31, cpuAmp: 20, mem: 72, disk: 61, diskTotal: 1e12, memTotal: 32 * 2 ** 30, cpuCount: 12, cpuModel: 'Intel Core i7-1365U', load: 1.5, rx: 340e3, tx: 120e3, platform: 'windows', platformVersion: '11 23H2', arch: 'amd64', uptimeDays: 3,
  },
  {
    id: 'nA1i5Piph4', name: 'alice-iphone', os: 'iOS', model: 'iPhone 15 Pro', user: 'alice@example.com', userDisplayName: 'Alice Park',
    ip4: '100.64.0.6', ip6: 'fd7a:115c:a1e0::6', online: false, offlineForMin: 184, path: 'none', agent: 'unknown', clientVersion: '1.82.0', createdDaysAgo: 290,
  },
  {
    id: 'nB0b6Xpxl5', name: 'bob-pixel', os: 'android', model: 'Pixel 8', user: 'bob@example.com', userDisplayName: 'Bob Reyes',
    ip4: '100.64.0.7', ip6: 'fd7a:115c:a1e0::7', online: true, path: 'relay', relay: 'fra', latency: 48, agent: 'unknown', clientVersion: '1.82.0', createdDaysAgo: 120, rx: 22e3, tx: 9e3,
    location: { city: 'Berlin', country: 'Germany', countryCode: 'DE', latitude: 52.52, longitude: 13.4 },
  },
  {
    id: 'nE9x7Cexit', name: 'edge-exit-1', os: 'linux', model: 'Hetzner CX22', user: 'tagged-devices', tags: ['tag:server'],
    ip4: '100.64.0.8', ip6: 'fd7a:115c:a1e0::8', online: true, path: 'direct', latency: 18, agent: 'reachable', clientVersion: '1.82.0', exitNode: true,
    advertisedRoutes: ['0.0.0.0/0', '::/0'], enabledRoutes: ['0.0.0.0/0', '::/0'], createdDaysAgo: 200, cpu: 27, cpuAmp: 18, mem: 92, disk: 44,
    diskTotal: 40e9, memTotal: 4 * 2 ** 30, cpuCount: 2, cpuModel: 'Intel Xeon (Skylake)', load: 0.9, rx: 3.1e6, tx: 3.4e6, platform: 'debian', platformVersion: '12', kernel: '6.1.0-25-cloud-amd64', arch: 'amd64', uptimeDays: 88,
    location: { city: 'Nuremberg', country: 'Germany', countryCode: 'DE', latitude: 49.45, longitude: 11.08 },
  },
  {
    id: 'nO3f4Rrtr6', name: 'office-router', os: 'linux', model: 'Protectli VP2420', user: 'tagged-devices', tags: ['tag:router'],
    ip4: '100.64.0.9', ip6: 'fd7a:115c:a1e0::9', online: true, path: 'direct', latency: 6.8, agent: 'reachable', clientVersion: '1.82.0',
    advertisedRoutes: ['10.10.0.0/16', '10.20.0.0/24'], enabledRoutes: ['10.10.0.0/16'], createdDaysAgo: 330, cpu: 6, cpuAmp: 6, mem: 29, disk: 22,
    diskTotal: 128e9, memTotal: 8 * 2 ** 30, cpuCount: 4, cpuModel: 'Intel Celeron J6412', load: 0.2, rx: 1.2e6, tx: 1.1e6, platform: 'debian', platformVersion: '12', kernel: '6.1.0-25-amd64', arch: 'amd64', uptimeDays: 201,
  },
  {
    id: 'nC5t6Lctr7', name: 'contractor-laptop', os: 'windows', model: 'Dell XPS 13', user: 'sam@partner.example', userDisplayName: 'Sam Okafor', isExternal: true,
    ip4: '100.64.0.10', ip6: 'fd7a:115c:a1e0::a', online: false, offlineForMin: 2 * 24 * 60 + 35, path: 'unknown', agent: 'disabled', clientVersion: '1.78.0', createdDaysAgo: 45,
  },
  {
    id: 'nN8w9Unew8', name: 'new-laptop-7f2a', os: 'macOS', model: 'MacBook Air M2', user: 'carol@example.com', userDisplayName: 'Carol Nguyen',
    ip4: '100.64.0.11', ip6: 'fd7a:115c:a1e0::b', online: true, path: 'direct', latency: 4.2, agent: 'unknown', clientVersion: '1.82.0', authorized: false, createdDaysAgo: 0.08,
  },
  {
    id: 'nM2d3Bmbx9', name: 'media-box', os: 'linux', model: 'Intel N100 mini PC', user: 'alice@example.com', userDisplayName: 'Alice Park',
    ip4: '100.64.0.12', ip6: 'fd7a:115c:a1e0::c', online: true, path: 'relay', relay: 'sfo', latency: 27, agent: 'reachable', clientVersion: '1.82.0', keyExpiryDays: 3,
    createdDaysAgo: 177, cpu: 14, cpuAmp: 30, mem: 38, disk: 83, diskTotal: 2e12, memTotal: 16 * 2 ** 30, cpuCount: 4, cpuModel: 'Intel N100', load: 0.6, rx: 4.8e6, tx: 260e3, platform: 'libreelec', platformVersion: '12.0', kernel: '6.6.47', arch: 'amd64', uptimeDays: 33,
  },
  {
    id: 'nB6k7Pbkp0', name: 'backup-server', os: 'linux', model: 'Dell PowerEdge T140', user: 'tagged-devices', tags: ['tag:server'],
    ip4: '100.64.0.13', ip6: 'fd7a:115c:a1e0::d', online: true, path: 'direct', latency: 1.9, agent: 'reachable', clientVersion: '1.60.1', updateAvailable: true, flapper: true,
    createdDaysAgo: 400, cpu: 21, cpuAmp: 40, mem: 49, disk: 77, diskTotal: 16e12, memTotal: 32 * 2 ** 30, cpuCount: 6, cpuModel: 'Intel Xeon E-2224', load: 1.1, rx: 6.2e6, tx: 480e3, platform: 'rocky', platformVersion: '9.4', kernel: '5.14.0-427.el9', arch: 'amd64', uptimeDays: 0.3,
  },
  {
    id: 'nD4v5Wdev1', name: 'dev-workstation', os: 'linux', model: 'Custom (Ryzen 9)', user: 'bob@example.com', userDisplayName: 'Bob Reyes',
    ip4: '100.64.0.14', ip6: 'fd7a:115c:a1e0::e', online: true, path: 'direct', latency: 0.9, agent: 'reachable', clientVersion: '1.82.0',
    createdDaysAgo: 260, cpu: 87, cpuAmp: 6, mem: 61, disk: 58, diskTotal: 4e12, memTotal: 64 * 2 ** 30, cpuCount: 32, cpuModel: 'AMD Ryzen 9 7950X', load: 27, rx: 1.4e6, tx: 900e3, platform: 'arch', platformVersion: 'rolling', kernel: '6.11.2-arch1-1', arch: 'amd64', uptimeDays: 12,
  },
  {
    id: 'nC1i2Rrun2', name: 'ci-runner-2', os: 'linux', model: 'AWS c6a.xlarge', user: 'tagged-devices', tags: ['tag:server', 'tag:ci'],
    ip4: '100.64.0.15', ip6: 'fd7a:115c:a1e0::f', online: true, path: 'direct', latency: 22, agent: 'reachable', clientVersion: '1.82.0',
    createdDaysAgo: 90, cpu: 64, cpuAmp: 30, mem: 58, disk: 49, diskTotal: 200e9, memTotal: 8 * 2 ** 30, cpuCount: 4, cpuModel: 'AMD EPYC 7R13', load: 8.9, rx: 5.5e6, tx: 2.2e6, platform: 'ubuntu', platformVersion: '22.04', kernel: '6.8.0-1015-aws', arch: 'amd64', uptimeDays: 41,
    location: { city: 'Ashburn', country: 'United States', countryCode: 'US', latitude: 39.04, longitude: -77.49 },
  },
  {
    id: 'nC3r4Ssrf3', name: 'carol-surface', os: 'windows', model: 'Surface Laptop 5', user: 'carol@example.com', userDisplayName: 'Carol Nguyen',
    ip4: '100.64.0.16', ip6: 'fd7a:115c:a1e0::10', online: true, path: 'direct', latency: 5.1, agent: 'unknown', clientVersion: '1.82.0', createdDaysAgo: 140, rx: 210e3, tx: 60e3,
  },
  {
    id: 'nH5l6Kk3s4', name: 'homelab-k3s', os: 'linux', model: 'Minisforum UM790', user: 'tagged-devices', tags: ['tag:server'],
    ip4: '100.64.0.17', ip6: 'fd7a:115c:a1e0::11', online: true, path: 'direct', latency: 1.1, agent: 'reachable', clientVersion: '1.82.0',
    createdDaysAgo: 150, cpu: 34, cpuAmp: 15, mem: 66, disk: 62, diskTotal: 2e12, memTotal: 64 * 2 ** 30, cpuCount: 16, cpuModel: 'AMD Ryzen 9 7940HS', load: 4.2, rx: 2.9e6, tx: 2.1e6, platform: 'ubuntu', platformVersion: '24.04', kernel: '6.8.0-45-generic', arch: 'amd64', uptimeDays: 97,
  },
]

export const HUB_ID = PROFILES[0]!.id

// ---------------------------------------------------------------------------
// Diurnal signal helpers
// ---------------------------------------------------------------------------

/** 0..1 diurnal factor peaking around 15:00 local time. */
function diurnal(tSeconds: number): number {
  const d = new Date(tSeconds * 1000)
  const h = d.getHours() + d.getMinutes() / 60
  return 0.5 + 0.5 * Math.sin(((h - 9) / 24) * Math.PI * 2)
}

/** Whether the flapping device is in its outage window (4 min every 40 min). */
export function flapperDown(tSeconds: number): boolean {
  return Math.floor(tSeconds / 60) % 40 >= 36
}

function profileSeed(p: Profile): number {
  return hashString(p.id)
}

interface Sample {
  online: boolean
  direct: boolean
  latency?: number
  tsRx: number
  tsTx: number
  cpu?: number
  mem?: number
  disk?: number
  load1?: number
  netRx?: number
  netTx?: number
  temp?: number
}

/** Deterministic sample for a profile at unix time t. */
export function sampleAt(p: Profile, t: number, now: number): Sample {
  const seed = profileSeed(p)
  const nowS = Math.floor(now / 1000)
  const d = diurnal(t)
  const slow = Math.sin(t / 1800 + seed % 7) * 0.5 + Math.sin(t / 420 + seed % 11) * 0.3
  const jitter = noise(seed, t)
  let online = p.online
  if (!p.online && p.offlineForMin !== undefined) {
    online = t < nowS - p.offlineForMin * 60
  }
  if (p.flapper && flapperDown(t)) online = false
  if (!online) {
    return { online: false, direct: false, tsRx: 0, tsTx: 0 }
  }
  const direct = p.path === 'direct'
  const latBase = p.latency ?? 5
  const latency = clamp(latBase * (1 + 0.15 * slow) + Math.abs(jitter) * latBase * 0.25 + (direct ? 0 : d * 6), 0.2, 900)
  const rxBase = p.rx ?? 50e3
  const txBase = p.tx ?? 20e3
  const burst = noise(seed + 1, t) > 0.92 ? 3.5 : 1
  const tsRx = Math.max(0, rxBase * (0.45 + 0.9 * d + 0.3 * slow) * burst + jitter * rxBase * 0.1)
  const tsTx = Math.max(0, txBase * (0.45 + 0.9 * d + 0.3 * slow) + noise(seed + 2, t) * txBase * 0.12)
  if (p.agent !== 'reachable') return { online, direct, latency, tsRx, tsTx }
  const cpu = clamp((p.cpu ?? 20) + (p.cpuAmp ?? 10) * (d - 0.5) * 2 + slow * 6 + jitter * 4, 0.5, 100)
  const mem = clamp((p.mem ?? 50) + slow * 2.5 + noise(seed + 3, t) * 0.8, 1, 100)
  const disk = clamp((p.disk ?? 40) + (t - (nowS - 86400 * 30)) / (86400 * 30) * 0.4, 1, 100)
  const cores = p.cpuCount ?? 4
  const load1 = Math.max(0.01, (p.load ?? 0.5) * (0.6 + 0.8 * d) + slow * 0.4 * (cores / 4) + jitter * 0.15)
  const netRx = tsRx * 2.4 + Math.max(0, noise(seed + 4, t)) * rxBase
  const netTx = tsTx * 2.1 + Math.max(0, noise(seed + 5, t)) * txBase
  const temp = p.temp !== undefined ? clamp(p.temp + (d - 0.5) * 8 + slow * 2 + jitter * 1.5, 20, 100) : undefined
  return { online, direct, latency, tsRx, tsTx, cpu, mem, disk, load1, netRx, netTx, temp }
}

// ---------------------------------------------------------------------------
// Device construction
// ---------------------------------------------------------------------------

function buildMetrics(p: Profile, s: Sample, now: number): MetricsSnapshot | undefined {
  if (p.agent !== 'reachable' || !s.online || s.cpu === undefined) return undefined
  const memTotal = p.memTotal ?? 8 * 2 ** 30
  const memUsed = Math.round(memTotal * ((s.mem ?? 50) / 100))
  const diskTotal = p.diskTotal ?? 256e9
  const diskUsed = Math.round(diskTotal * ((s.disk ?? 40) / 100))
  const cores = p.cpuCount ?? 4
  const seed = profileSeed(p)
  const perCore = Array.from({ length: Math.min(cores, 32) }, (_, i) => clamp((s.cpu ?? 0) + noise(seed + i, Math.floor(now / 5000)) * 18, 0, 100))
  const uptimeSeconds = Math.round((p.uptimeDays ?? 10) * 86400 + (now % 86400000) / 1000)
  const disks = [{ mount: '/', fstype: 'ext4', total: diskTotal, used: diskUsed, percent: s.disk ?? 0 }]
  if (p.name === 'nas-basement') disks.push({ mount: '/volume1', fstype: 'btrfs', total: 22e12, used: 20.1e12, percent: 91.4 })
  return {
    sampledAt: iso(now),
    platform: p.platform,
    platformVersion: p.platformVersion,
    kernel: p.kernel,
    arch: p.arch,
    bootTime: iso(now - uptimeSeconds * 1000),
    uptimeSeconds,
    cpuPercent: s.cpu ?? 0,
    cpuCount: cores,
    cpuModel: p.cpuModel,
    perCore,
    load1: s.load1 ?? 0,
    load5: (s.load1 ?? 0) * 0.92,
    load15: (s.load1 ?? 0) * 0.85,
    memTotal,
    memUsed,
    memAvailable: memTotal - memUsed,
    memPercent: s.mem ?? 0,
    swapTotal: 2 * 2 ** 30,
    swapUsed: Math.round(2 * 2 ** 30 * 0.12),
    disks,
    diskPercent: s.disk ?? 0,
    interfaces: [
      { name: p.os === 'macOS' ? 'en0' : 'eth0', rxBytes: 812e9, txBytes: 402e9, rxPackets: 9.1e8, txPackets: 6.2e8, rxErrors: 0, txErrors: 0, rxRate: s.netRx ?? 0, txRate: s.netTx ?? 0 },
      { name: p.os === 'macOS' ? 'utun4' : 'tailscale0', rxBytes: 143e9, txBytes: 88e9, rxPackets: 1.4e8, txPackets: 1.1e8, rxErrors: 0, txErrors: 0, rxRate: s.tsRx, txRate: s.tsTx },
    ],
    tailscaleInterface: p.os === 'macOS' ? 'utun4' : 'tailscale0',
    netRxRate: s.netRx ?? 0,
    netTxRate: s.netTx ?? 0,
    temperatures: s.temp !== undefined ? [{ sensor: 'cpu_thermal', celsius: s.temp, critical: 85 }] : undefined,
    processes: 140 + Math.round(Math.abs(noise(seed + 99, Math.floor(now / 60000))) * 60),
    tailscaleVersion: p.clientVersion,
  }
}

export function buildDevice(p: Profile, now: number): Device {
  const nowS = Math.floor(now / 1000)
  const s = sampleAt(p, nowS, now)
  const online = s.online
  const lastSeen = online ? iso(now - 4000) : iso(now - (p.offlineForMin ?? 5) * 60000)
  const created = iso(now - p.createdDaysAgo * 86400000)
  const keyExpiry = p.keyExpiryDisabled ? undefined : iso(now + (p.keyExpiryDays ?? 120) * 86400000)
  const derpLatency: Record<string, number> = { nyc: 12 + (hashString(p.id) % 9), fra: 88 + (hashString(p.id) % 20), sfo: 31 + (hashString(p.id) % 12) }
  const preferred = p.relay ?? 'nyc'
  return {
    id: p.id,
    name: p.name,
    dnsName: `${p.name}.${MAGIC_DNS_SUFFIX}`,
    hostname: p.name,
    os: p.os,
    deviceModel: p.model,
    addresses: [p.ip4, p.ip6],
    user: p.user,
    userDisplayName: p.userDisplayName,
    tags: p.tags ?? [],
    isSelf: !!p.isSelf,
    isExternal: !!p.isExternal,
    online,
    active: online && s.tsRx > 1000,
    lastSeen,
    lastHandshake: online ? iso(now - 60000 * (1 + (hashString(p.id) % 4))) : undefined,
    created,
    firstSeen: created,
    updatedAt: iso(now),
    clientVersion: p.clientVersion,
    updateAvailable: !!p.updateAvailable,
    authorized: p.authorized ?? true,
    keyExpiry,
    keyExpiryDisabled: !!p.keyExpiryDisabled,
    expired: false,
    exitNodeOption: !!p.exitNode,
    isExitNode: !!p.exitNode,
    advertisedRoutes: p.advertisedRoutes ?? [],
    enabledRoutes: p.enabledRoutes ?? [],
    primaryRoutes: p.enabledRoutes ?? [],
    blocksIncomingConnections: p.os === 'iOS' || p.os === 'android',
    location: p.location,
    connectivity: {
      path: online ? p.path : 'none',
      relay: online && p.path === 'relay' ? p.relay : undefined,
      curAddr: online && p.path === 'direct' ? `203.0.113.${10 + (hashString(p.id) % 200)}:41641` : undefined,
      endpoints: [`203.0.113.${10 + (hashString(p.id) % 200)}:41641`, `192.168.1.${20 + (hashString(p.id) % 200)}:41641`],
      latencyMs: online ? s.latency : undefined,
      lastPing: online ? iso(now - 12000) : undefined,
      rxBytes: 143e9 * (0.2 + (hashString(p.id) % 10) / 10),
      txBytes: 88e9 * (0.2 + (hashString(p.id) % 7) / 10),
      rxRate: s.tsRx,
      txRate: s.tsTx,
      derpLatencyMs: derpLatency,
      preferredDerp: preferred,
      natSupport: { hairPinning: true, ipv6: p.path === 'direct', pcp: false, pmp: p.path === 'direct', udp: true, upnp: p.path === 'direct' },
      mappingVariesByDestIp: p.path === 'relay',
    },
    agent: {
      state: p.agent,
      version: p.agent === 'reachable' ? '0.4.2' : undefined,
      lastSuccess: p.agent === 'reachable' ? iso(now - 6000) : p.agent === 'unreachable' ? iso(now - 3600000 * 4) : undefined,
      lastError: p.agent === 'unreachable' ? p.agentError : undefined,
      url: p.agent === 'reachable' || p.agent === 'unreachable' ? `http://${p.ip4}:41820/v1/metrics` : undefined,
    },
    metrics: buildMetrics(p, s, now),
    uptime: {
      pct24h: p.flapper ? 90.1 : !p.online ? (p.offlineForMin ?? 0) >= 1440 ? 0 : 100 - ((p.offlineForMin ?? 0) / 1440) * 100 : 99.98 + (hashString(p.id) % 3) / 100,
      pct7d: p.flapper ? 90.2 : !p.online ? 71.4 : 99.91 + (hashString(p.id) % 8) / 100,
      pct30d: p.flapper ? 90.6 : !p.online ? 93.2 : 99.87 + (hashString(p.id) % 12) / 100,
      lastChange: online ? iso(now - (p.uptimeDays ?? 10) * 86400000 * 0.7) : lastSeen,
      onlineForSeconds: online ? Math.round((p.uptimeDays ?? 10) * 86400 * 0.7) : undefined,
    },
  }
}

// ---------------------------------------------------------------------------
// Series / uptime
// ---------------------------------------------------------------------------

export function buildSeries(p: Profile, range: RangeKey, now: number): Series {
  const nowS = Math.floor(now / 1000)
  const step = rangeStepSeconds(range)
  const span = rangeSeconds(range)
  const from = Math.floor((nowS - span) / step) * step
  const points: SeriesPoint[] = []
  for (let t = from; t <= nowS; t += step) {
    const s = sampleAt(p, t, now)
    if (!s.online) {
      points.push({ t, online: 0, tsRxRate: 0, tsTxRate: 0 })
      continue
    }
    const pt: SeriesPoint = {
      t,
      online: 1,
      direct: s.direct ? 1 : 0,
      latencyMs: s.latency,
      latencyMax: s.latency !== undefined ? s.latency * (1.1 + Math.abs(noise(7, t)) * 0.6) : undefined,
      tsRxRate: s.tsRx,
      tsTxRate: s.tsTx,
    }
    if (s.cpu !== undefined) {
      pt.cpu = s.cpu
      pt.cpuMax = clamp(s.cpu * 1.15 + 3, 0, 100)
      pt.mem = s.mem
      pt.disk = s.disk
      pt.load1 = s.load1
      pt.netRxRate = s.netRx
      pt.netTxRate = s.netTx
      if (s.temp !== undefined) pt.tempC = s.temp
    }
    points.push(pt)
  }
  return {
    deviceId: p.id,
    from: iso(from * 1000),
    to: iso(nowS * 1000),
    stepSeconds: step,
    source: span > 48 * 3600 ? 'rollup' : 'raw',
    points,
  }
}

export function buildUptime(p: Profile, range: RangeKey, now: number): UptimeReport {
  const nowS = Math.floor(now / 1000)
  const span = rangeSeconds(range)
  const step = Math.max(60, rangeStepSeconds(range))
  const from = nowS - span
  const segments: TimelineSegment[] = []
  let cur: { from: number; online: boolean } | null = null
  let onlineSec = 0
  let outages = 0
  for (let t = from; t <= nowS; t += step) {
    const on = sampleAt(p, t, now).online
    if (on) onlineSec += step
    if (!cur) cur = { from: t, online: on }
    else if (cur.online !== on) {
      segments.push({ from: iso(cur.from * 1000), to: iso(t * 1000), online: cur.online })
      if (!on) outages++
      cur = { from: t, online: on }
    }
  }
  if (cur) segments.push({ from: iso(cur.from * 1000), to: iso(nowS * 1000), online: cur.online })
  return {
    deviceId: p.id,
    from: iso(from * 1000),
    to: iso(nowS * 1000),
    pct: Math.min(100, (onlineSec / span) * 100),
    segments,
    outages,
  }
}

// ---------------------------------------------------------------------------
// Rules / alerts / events / audit
// ---------------------------------------------------------------------------

const RULE_DEFS: Array<[AlertRuleType, string, string, Severity, number, number]> = [
  ['device_offline', 'Device offline', 'A device has been offline for longer than the grace period.', 'warning', 0, 300],
  ['high_cpu', 'High CPU', 'CPU usage sustained above the threshold.', 'warning', 90, 600],
  ['high_memory', 'High memory', 'Memory usage sustained above the threshold.', 'warning', 90, 600],
  ['disk_full', 'Disk nearly full', 'Any filesystem above the threshold; escalates to critical at 97%.', 'warning', 90, 0],
  ['high_latency', 'High latency', 'Tailscale ping latency sustained above the threshold.', 'info', 250, 300],
  ['relay_only', 'Relay only', 'Device has used a DERP relay (no direct path) for longer than the threshold.', 'info', 0, 900],
  ['key_expiring', 'Key expiring', 'Node key expires within the threshold; critical under 2 days.', 'warning', 7, 0],
  ['update_available', 'Update available', 'A newer Tailscale client is available.', 'info', 0, 0],
  ['agent_unreachable', 'Agent unreachable', 'The tailwatch-agent on a device stopped responding.', 'warning', 0, 300],
  ['new_device', 'New device', 'A device joined the tailnet; auto-resolves after the window.', 'info', 0, 86400],
  ['unauthorized_device', 'Unauthorized device', 'A device is waiting for authorization.', 'warning', 0, 0],
  ['high_temperature', 'High temperature', 'A temperature sensor sustained above the threshold.', 'warning', 85, 300],
  ['high_load', 'High load', 'load1 / cpuCount sustained above the ratio.', 'info', 2, 600],
]

export function buildRules(now: number): AlertRule[] {
  return RULE_DEFS.map(([type, name, description, severity, threshold, forSeconds]) => ({
    id: type,
    type,
    name,
    description,
    enabled: true,
    severity,
    threshold,
    forSeconds,
    notify: severity !== 'info',
    updatedAt: iso(now - 14 * 86400000),
  }))
}

function dev(name: string): Profile {
  const p = PROFILES.find((x) => x.name === name)
  if (!p) throw new Error(`unknown profile ${name}`)
  return p
}

export function buildAlerts(now: number): Alert[] {
  const m = (min: number) => iso(now - min * 60000)
  const nas = dev('nas-basement'), exit = dev('edge-exit-1'), media = dev('media-box'), nl = dev('new-laptop-7f2a')
  const bk = dev('backup-server'), pi = dev('pi-garage'), dw = dev('dev-workstation'), bt = dev('bob-thinkpad')
  return [
    { id: 1041, ruleId: 'disk_full', ruleType: 'disk_full', deviceId: nas.id, deviceName: nas.name, state: 'open', severity: 'warning', title: 'Disk nearly full on nas-basement', message: '/volume1 is 91% full (20.1 TiB of 22 TiB).', value: 91.4, openedAt: m(6 * 60 + 12), updatedAt: m(2), data: { mount: '/volume1' } },
    { id: 1042, ruleId: 'high_memory', ruleType: 'high_memory', deviceId: exit.id, deviceName: exit.name, state: 'open', severity: 'warning', title: 'High memory on edge-exit-1', message: 'Memory usage has been above 90% for 47 minutes.', value: 92.3, openedAt: m(47), updatedAt: m(1) },
    { id: 1043, ruleId: 'key_expiring', ruleType: 'key_expiring', deviceId: media.id, deviceName: media.name, state: 'open', severity: 'warning', title: 'Node key expiring on media-box', message: 'The node key expires in 3 days. Re-authenticate or disable key expiry.', value: 3, openedAt: m(4 * 24 * 60), updatedAt: m(30) },
    { id: 1044, ruleId: 'unauthorized_device', ruleType: 'unauthorized_device', deviceId: nl.id, deviceName: nl.name, state: 'open', severity: 'warning', title: 'Unauthorized device new-laptop-7f2a', message: 'A new macOS device is waiting for admin approval.', openedAt: m(118), updatedAt: m(118) },
    { id: 1045, ruleId: 'update_available', ruleType: 'update_available', deviceId: bk.id, deviceName: bk.name, state: 'open', severity: 'info', title: 'Update available for backup-server', message: 'Client 1.60.1 is outdated; 1.82.0 is available.', openedAt: m(3 * 24 * 60), updatedAt: m(3 * 24 * 60), ackedAt: m(2 * 24 * 60), ackedBy: 'alice@example.com' },
    { id: 1046, ruleId: 'agent_unreachable', ruleType: 'agent_unreachable', deviceId: bt.id, deviceName: bt.name, state: 'open', severity: 'warning', title: 'Agent unreachable on bob-thinkpad', message: 'dial tcp 100.64.0.5:41820: connection refused', openedAt: m(4 * 60 + 3), updatedAt: m(5) },
    { id: 1040, ruleId: 'device_offline', ruleType: 'device_offline', deviceId: pi.id, deviceName: pi.name, state: 'resolved', severity: 'warning', title: 'pi-garage offline', message: 'Device was offline for 23 minutes.', openedAt: m(9 * 60 + 40), updatedAt: m(9 * 60 + 17), resolvedAt: m(9 * 60 + 17) },
    { id: 1039, ruleId: 'high_cpu', ruleType: 'high_cpu', deviceId: dw.id, deviceName: dw.name, state: 'resolved', severity: 'warning', title: 'High CPU on dev-workstation', message: 'CPU stayed above 90% for 38 minutes.', value: 96.2, openedAt: m(13 * 60), updatedAt: m(12 * 60 + 22), resolvedAt: m(12 * 60 + 22), ackedAt: m(12 * 60 + 50), ackedBy: 'bob@example.com' },
    { id: 1038, ruleId: 'device_offline', ruleType: 'device_offline', deviceId: bk.id, deviceName: bk.name, state: 'resolved', severity: 'warning', title: 'backup-server offline', message: 'Device was offline for 4 minutes.', openedAt: m(52), updatedAt: m(48), resolvedAt: m(48) },
  ]
}

export function buildEvents(now: number): Event[] {
  const m = (min: number) => iso(now - min * 60000)
  const rows: Array<[number, EventType, Severity, string | null, string, string?]> = [
    [1, 'device.path_changed', 'info', 'bob-pixel', 'bob-pixel switched to relay fra', 'Direct path lost; now relayed via DERP fra (48 ms).'],
    [4, 'device.online', 'info', 'backup-server', 'backup-server came online'],
    [5, 'alert.resolved', 'info', 'backup-server', 'Resolved: backup-server offline', 'Device was offline for 4 minutes.'],
    [8, 'device.offline', 'warning', 'backup-server', 'backup-server went offline'],
    [21, 'device.updated', 'info', 'alice-mbp', 'alice-mbp client updated', 'Client version 1.80.2 → 1.80.3.'],
    [33, 'admin.action', 'info', 'office-router', 'Routes changed on office-router', 'alice@example.com enabled 10.10.0.0/16.'],
    [47, 'alert.opened', 'warning', 'edge-exit-1', 'High memory on edge-exit-1', 'Memory usage has been above 90% for 10 minutes.'],
    [52, 'device.offline', 'warning', 'backup-server', 'backup-server went offline'],
    [56, 'device.online', 'info', 'backup-server', 'backup-server came online'],
    [74, 'device.path_changed', 'info', 'pi-garage', 'pi-garage switched to relay nyc', 'Direct path lost; now relayed via DERP nyc (34 ms).'],
    [92, 'device.online', 'info', 'backup-server', 'backup-server came online'],
    [96, 'device.offline', 'warning', 'backup-server', 'backup-server went offline'],
    [118, 'alert.opened', 'warning', 'new-laptop-7f2a', 'Unauthorized device new-laptop-7f2a', 'A new macOS device is waiting for admin approval.'],
    [118, 'device.new', 'info', 'new-laptop-7f2a', 'New device new-laptop-7f2a joined', 'carol@example.com · macOS · MacBook Air M2'],
    [132, 'device.online', 'info', 'backup-server', 'backup-server came online'],
    [136, 'device.offline', 'warning', 'backup-server', 'backup-server went offline'],
    [184, 'device.offline', 'info', 'alice-iphone', 'alice-iphone went offline'],
    [243, 'agent.unreachable', 'warning', 'bob-thinkpad', 'Agent on bob-thinkpad is unreachable', 'dial tcp 100.64.0.5:41820: connection refused'],
    [243, 'alert.opened', 'warning', 'bob-thinkpad', 'Agent unreachable on bob-thinkpad', 'dial tcp 100.64.0.5:41820: connection refused'],
    [255, 'device.updated', 'info', 'carol-surface', 'carol-surface client updated', 'Client version 1.80.3 → 1.82.0.'],
    [301, 'device.online', 'info', 'alice-iphone', 'alice-iphone came online'],
    [372, 'alert.opened', 'warning', 'nas-basement', 'Disk nearly full on nas-basement', '/volume1 is 90% full.'],
    [388, 'admin.action', 'info', 'media-box', 'Ping media-box', 'alice@example.com pinged media-box: 27 ms via relay sfo.'],
    [402, 'device.path_changed', 'info', 'media-box', 'media-box switched to relay sfo', 'Direct path lost; now relayed via DERP sfo (27 ms).'],
    [451, 'device.online', 'info', 'bob-pixel', 'bob-pixel came online'],
    [497, 'device.offline', 'info', 'bob-pixel', 'bob-pixel went offline'],
    [557, 'alert.resolved', 'info', 'pi-garage', 'Resolved: pi-garage offline', 'Device was offline for 23 minutes.'],
    [557, 'device.online', 'info', 'pi-garage', 'pi-garage came online'],
    [580, 'alert.opened', 'warning', 'pi-garage', 'pi-garage offline', 'Device has been offline for 5 minutes.'],
    [580, 'device.offline', 'warning', 'pi-garage', 'pi-garage went offline'],
    [614, 'admin.action', 'info', 'nas-basement', 'Tags changed on nas-basement', 'alice@example.com set tags: tag:nas.'],
    [702, 'device.updated', 'info', 'netwatch-hub', 'netwatch-hub client updated', 'Client version 1.80.3 → 1.82.0.'],
    [742, 'alert.resolved', 'info', 'dev-workstation', 'Resolved: High CPU on dev-workstation', 'CPU dropped below 90%.'],
    [770, 'alert.acked', 'info', 'dev-workstation', 'Acknowledged: High CPU on dev-workstation', 'bob@example.com acknowledged the alert.'],
    [780, 'alert.opened', 'warning', 'dev-workstation', 'High CPU on dev-workstation', 'CPU has been above 90% for 10 minutes.'],
    [861, 'device.online', 'info', 'carol-surface', 'carol-surface came online'],
    [1020, 'device.offline', 'info', 'carol-surface', 'carol-surface went offline'],
    [1195, 'hub.error', 'warning', null, 'Control API poll failed', 'GET /api/v2/tailnet/example.com/devices: 502 Bad Gateway (retried).'],
    [1230, 'device.updated', 'info', 'homelab-k3s', 'homelab-k3s client updated', 'Client version 1.80.3 → 1.82.0.'],
    [1387, 'device.online', 'info', 'contractor-laptop', 'contractor-laptop came online'],
    [1421, 'hub.started', 'info', null, 'Tailwatch hub started', 'Version 0.1.0 on netwatch-hub · poll interval 15s.'],
  ]
  const out: Event[] = []
  let id = 9000
  for (const [min, type, severity, device, title, message] of rows) {
    const p = device ? dev(device) : null
    out.push({ id: id--, ts: m(min), type, severity, deviceId: p?.id, deviceName: p?.name, title, message })
  }
  return out.sort((a, b) => (a.ts < b.ts ? 1 : -1))
}

export function buildAudit(now: number): AuditEntry[] {
  const m = (min: number) => iso(now - min * 60000)
  return [
    { id: 71, ts: m(33), actor: 'alice@example.com', actorNode: 'alice-mbp', action: 'device.routes', target: dev('office-router').id, details: { routes: ['10.10.0.0/16'] }, ok: true, remoteIp: '100.64.0.4' },
    { id: 70, ts: m(388), actor: 'alice@example.com', actorNode: 'alice-mbp', action: 'device.ping', target: dev('media-box').id, details: { latencyMs: 27.4, path: 'relay' }, ok: true, remoteIp: '100.64.0.4' },
    { id: 69, ts: m(614), actor: 'alice@example.com', actorNode: 'netwatch-hub', action: 'device.tags', target: dev('nas-basement').id, details: { tags: ['tag:nas'] }, ok: true, remoteIp: '100.64.0.1' },
    { id: 68, ts: m(770), actor: 'bob@example.com', actorNode: 'dev-workstation', action: 'alert.ack', target: '1039', ok: true, remoteIp: '100.64.0.14' },
    { id: 67, ts: m(2 * 24 * 60), actor: 'alice@example.com', actorNode: 'alice-mbp', action: 'alert.ack', target: '1045', ok: true, remoteIp: '100.64.0.4' },
    { id: 66, ts: m(2 * 24 * 60 + 40), actor: 'bob@example.com', actorNode: 'bob-thinkpad', action: 'device.authorize', target: dev('contractor-laptop').id, ok: false, error: 'control API: 403 Forbidden (key lacks devices:write)', remoteIp: '100.64.0.5' },
    { id: 65, ts: m(3 * 24 * 60), actor: 'alice@example.com', actorNode: 'alice-mbp', action: 'rule.save', target: 'high_latency', details: { threshold: 250, forSeconds: 300 }, ok: true, remoteIp: '100.64.0.4' },
    { id: 64, ts: m(5 * 24 * 60), actor: 'alice@example.com', actorNode: 'alice-mbp', action: 'alerts.test', target: 'webhook', details: { sent: ['webhook'] }, ok: true, remoteIp: '100.64.0.4' },
  ]
}

// ---------------------------------------------------------------------------
// Identity / hub / settings
// ---------------------------------------------------------------------------

export function buildIdentity(): Identity {
  return {
    login: 'demo@example.com',
    displayName: 'Demo Admin',
    nodeName: 'netwatch-hub',
    nodeId: HUB_ID,
    nodeIp: '100.64.0.1',
    tags: [],
    role: 'admin',
    authMode: 'none',
  }
}

export function buildHub(now: number, startedAt: number): HubInfo {
  return {
    version: '0.1.0-demo',
    tailnet: TAILNET,
    magicDnsSuffix: MAGIC_DNS_SUFFIX,
    selfName: 'netwatch-hub',
    selfId: HUB_ID,
    selfIps: ['100.64.0.1', 'fd7a:115c:a1e0::1'],
    tailscaleVersion: '1.82.0',
    backendState: 'Running',
    health: [],
    startedAt: iso(startedAt),
    demoMode: true,
    controlApiEnabled: true,
    adminActionsEnabled: true,
    agentPort: 41820,
    pollIntervalSeconds: 15,
    lastPoll: iso(now),
    lastApiPoll: iso(now - 30000),
  }
}

export function buildSettings(now: number, state: { devices: Device[]; events: Event[]; alerts: Alert[] }): Settings {
  return {
    authMode: 'none',
    adminUsers: ['alice@example.com'],
    adminTags: ['tag:admin'],
    viewerUsers: ['*'],
    viewerTags: [],
    adminActionsEnabled: true,
    controlApiEnabled: true,
    tailnet: TAILNET,
    pollIntervalSeconds: 15,
    apiIntervalSeconds: 60,
    pingIntervalSeconds: 30,
    agentPort: 41820,
    agentEnabled: true,
    rawRetentionHours: 48,
    rollupRetentionDays: 90,
    eventRetentionDays: 90,
    notifiers: ['webhook', 'ntfy'],
    listen: '100.64.0.1:8484',
    demoMode: true,
    store: {
      devices: state.devices.length,
      samples: 5760 * state.devices.length,
      rollups: 8640 * state.devices.length,
      events: state.events.length + 1312,
      alerts: state.alerts.length + 87,
      sizeBytes: 58.3 * 2 ** 20,
      oldestSample: iso(now - 48 * 3600000),
    },
  }
}

// ---------------------------------------------------------------------------
// Overview / topology
// ---------------------------------------------------------------------------

export function buildOverview(devices: Device[], alerts: Alert[], hub: HubInfo, now: number): Overview {
  const online = devices.filter((d) => d.online)
  const osBreakdown: Record<string, number> = {}
  const userBreakdown: Record<string, number> = {}
  for (const d of devices) {
    osBreakdown[d.os] = (osBreakdown[d.os] ?? 0) + 1
    userBreakdown[d.user] = (userBreakdown[d.user] ?? 0) + 1
  }
  const lat = online.map((d) => d.connectivity.latencyMs).filter((v): v is number => typeof v === 'number')
  const open = alerts.filter((a) => a.state === 'open')
  const nowS = Math.floor(now / 1000)
  const step = 900
  const n = 96
  const t: number[] = []
  const on: number[] = []
  const rx: number[] = []
  const tx: number[] = []
  const latency: number[] = []
  for (let i = n - 1; i >= 0; i--) {
    const ts = nowS - i * step
    let onl = 0, r = 0, x = 0, lsum = 0, lc = 0
    for (const p of PROFILES) {
      const s = sampleAt(p, ts, now)
      if (s.online) {
        onl++
        r += s.tsRx
        x += s.tsTx
        if (s.latency !== undefined) {
          lsum += s.latency
          lc++
        }
      }
    }
    t.push(ts)
    on.push(onl)
    rx.push(Math.round(r))
    tx.push(Math.round(x))
    latency.push(lc ? Math.round((lsum / lc) * 10) / 10 : 0)
  }
  return {
    hub,
    devices: devices.length,
    online: online.length,
    offline: devices.length - online.length,
    direct: online.filter((d) => d.connectivity.path === 'direct').length,
    relayed: online.filter((d) => d.connectivity.path === 'relay').length,
    agentsReachable: devices.filter((d) => d.agent.state === 'reachable').length,
    updatesPending: devices.filter((d) => d.updateAvailable).length,
    keysExpiringSoon: devices.filter((d) => d.keyExpiry && !d.keyExpiryDisabled && new Date(d.keyExpiry).getTime() - now < 7 * 86400000).length,
    unauthorized: devices.filter((d) => !d.authorized).length,
    exitNodes: devices.filter((d) => d.isExitNode).length,
    subnetRouters: devices.filter((d) => d.enabledRoutes.some((r) => !r.startsWith('0.0.0.0') && !r.startsWith('::'))).length,
    openAlerts: open.length,
    criticalAlerts: open.filter((a) => a.severity === 'critical').length,
    totalRxRate: online.reduce((s, d) => s + d.connectivity.rxRate, 0),
    totalTxRate: online.reduce((s, d) => s + d.connectivity.txRate, 0),
    totalRxBytes: devices.reduce((s, d) => s + d.connectivity.rxBytes, 0),
    totalTxBytes: devices.reduce((s, d) => s + d.connectivity.txBytes, 0),
    avgLatencyMs: lat.length ? lat.reduce((a, b) => a + b, 0) / lat.length : undefined,
    osBreakdown,
    userBreakdown,
    sparklines: { t, online: on, rxRate: rx, txRate: tx, latency },
  }
}

export function buildTopology(devices: Device[]): Topology {
  const nodes: TopologyNode[] = devices.map((d) => ({
    id: d.id,
    name: d.name,
    os: d.os,
    online: d.online,
    isSelf: d.isSelf,
    isExitNode: d.isExitNode,
    isSubnetRouter: d.enabledRoutes.some((r) => !r.startsWith('0.0.0.0') && !r.startsWith('::')),
    tags: d.tags,
    user: d.user,
    relay: d.connectivity.relay,
    path: d.connectivity.path,
    latencyMs: d.connectivity.latencyMs,
  }))
  const edges: TopologyEdge[] = devices
    .filter((d) => !d.isSelf && d.online)
    .map((d) => ({
      from: HUB_ID,
      to: d.id,
      path: d.connectivity.path,
      relay: d.connectivity.relay,
      latencyMs: d.connectivity.latencyMs,
      rxRate: d.connectivity.rxRate,
      txRate: d.connectivity.txRate,
    }))
  return {
    nodes,
    edges,
    derpRegions: { nyc: 'New York City', fra: 'Frankfurt', sfo: 'San Francisco', lhr: 'London', tok: 'Tokyo' },
  }
}

// ---------------------------------------------------------------------------
// Mutable state + tick simulation
// ---------------------------------------------------------------------------

export interface MockState {
  startedAt: number
  tick: number
  identity: Identity
  hub: HubInfo
  devices: Device[]
  events: Event[]
  alerts: Alert[]
  rules: AlertRule[]
  audit: AuditEntry[]
  nextEventId: number
  nextAlertId: number
  nextAuditId: number
  /** Live overrides applied by mutations (rename, tags, …) keyed by device id. */
  overrides: Map<string, Partial<Device>>
  deleted: Set<string>
}

export function createMockState(now: number = Date.now()): MockState {
  const startedAt = now - 6 * 3600000 - 1421 * 60000 + 6 * 3600000
  const hub = buildHub(now, startedAt)
  const devices = PROFILES.map((p) => buildDevice(p, now))
  const events = buildEvents(now)
  const alerts = buildAlerts(now)
  return {
    startedAt,
    tick: 0,
    identity: buildIdentity(),
    hub,
    devices,
    events,
    alerts,
    rules: buildRules(now),
    audit: buildAudit(now),
    nextEventId: 9100,
    nextAlertId: 1050,
    nextAuditId: 72,
    overrides: new Map(),
    deleted: new Set(),
  }
}

/** Re-derive the device list for `now`, applying mutation overrides and deletions. */
export function refreshDevices(state: MockState, now: number): Device[] {
  state.devices = PROFILES.filter((p) => !state.deleted.has(p.id)).map((p) => {
    const base = buildDevice(p, now)
    const o = state.overrides.get(p.id)
    return o ? { ...base, ...o, connectivity: base.connectivity, metrics: base.metrics, uptime: base.uptime } : base
  })
  return state.devices
}

export function findProfile(idOrName: string): Profile | undefined {
  const s = idOrName.toLowerCase()
  return PROFILES.find((p) => p.id === idOrName || p.name.toLowerCase() === s || `${p.name}.${MAGIC_DNS_SUFFIX}`.toLowerCase() === s)
}

export function findDevice(state: MockState, idOrName: string): Device | undefined {
  const p = findProfile(idOrName)
  if (!p || state.deleted.has(p.id)) return undefined
  return state.devices.find((d) => d.id === p.id)
}

export function overview(state: MockState, now: number): Overview {
  state.hub = { ...state.hub, lastPoll: iso(now), lastApiPoll: iso(now - 20000) }
  return buildOverview(state.devices, state.alerts, state.hub, now)
}

export function deviceDetail(state: MockState, d: Device, now: number): DeviceDetail {
  const p = findProfile(d.id)!
  return {
    device: d,
    recentEvents: state.events.filter((e) => e.deviceId === d.id).slice(0, 20),
    openAlerts: state.alerts.filter((a) => a.deviceId === d.id && a.state === 'open'),
    uptime24h: buildUptime(p, '24h', now),
  }
}

export interface TickResult {
  devices: Device[]
  overview: Overview
  events: Event[]
  alerts: Alert[]
}

/** Advance the simulation one poll: refresh metrics, flap the flapper, occasionally emit events. */
export function advance(state: MockState, now: number): TickResult {
  const prev = new Map(state.devices.map((d) => [d.id, d]))
  state.tick++
  const devices = refreshDevices(state, now)
  const events: Event[] = []
  const alerts: Alert[] = []
  const ts = iso(now)
  for (const d of devices) {
    const before = prev.get(d.id)
    if (!before) continue
    if (before.online !== d.online) {
      events.push({
        id: state.nextEventId++,
        ts,
        type: d.online ? 'device.online' : 'device.offline',
        severity: d.online ? 'info' : 'warning',
        deviceId: d.id,
        deviceName: d.name,
        title: d.online ? `${d.name} came online` : `${d.name} went offline`,
      })
      if (!d.online) {
        const a: Alert = {
          id: state.nextAlertId++,
          ruleId: 'device_offline',
          ruleType: 'device_offline',
          deviceId: d.id,
          deviceName: d.name,
          state: 'open',
          severity: 'warning',
          title: `${d.name} offline`,
          message: 'Device has been offline for 5 minutes.',
          openedAt: ts,
          updatedAt: ts,
        }
        state.alerts.unshift(a)
        alerts.push(a)
        events.push({ id: state.nextEventId++, ts, type: 'alert.opened', severity: 'warning', deviceId: d.id, deviceName: d.name, title: a.title, message: a.message })
      } else {
        const open = state.alerts.find((a) => a.deviceId === d.id && a.ruleType === 'device_offline' && a.state === 'open')
        if (open) {
          open.state = 'resolved'
          open.resolvedAt = ts
          open.updatedAt = ts
          alerts.push({ ...open })
          events.push({ id: state.nextEventId++, ts, type: 'alert.resolved', severity: 'info', deviceId: d.id, deviceName: d.name, title: `Resolved: ${open.title}`, message: 'Device is back online.' })
        }
      }
    }
  }
  // A gentle stream of informational events so feeds feel alive.
  if (state.tick % 9 === 0) {
    const pool = devices.filter((d) => d.online && !d.isSelf)
    const d = pool[state.tick % pool.length]
    if (d) {
      const relay = d.connectivity.path === 'relay'
      events.push({
        id: state.nextEventId++,
        ts,
        type: 'device.path_changed',
        severity: 'info',
        deviceId: d.id,
        deviceName: d.name,
        title: relay ? `${d.name} path check: relay ${d.connectivity.relay ?? ''}`.trim() : `${d.name} path check: direct`,
        message: relay ? `Still relayed via DERP ${d.connectivity.relay ?? ''} (${Math.round(d.connectivity.latencyMs ?? 0)} ms).` : `Direct path confirmed (${(d.connectivity.latencyMs ?? 0).toFixed(1)} ms).`,
      })
    }
  }
  for (const e of events) state.events.unshift(e)
  if (state.events.length > 500) state.events.length = 500
  return { devices, overview: overview(state, now), events, alerts }
}
