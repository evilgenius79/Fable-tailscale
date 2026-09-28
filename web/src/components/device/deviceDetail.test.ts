import { describe, expect, it } from 'vitest'
import type { Device, SeriesPoint, UptimeReport } from '../../api/types'
import {
  agentInstallCommand,
  pingResultToast,
  derpRows,
  deviceBadges,
  interfaceRows,
  isExitRoute,
  loadPercent,
  natRows,
  normalizeTag,
  outageList,
  routeRows,
  seriesHas,
  seriesRows,
  timelineBars,
  timelineTicks,
  trend,
  validateDeviceName,
  validateTag,
} from './deviceDetail'

const baseDevice = (over: Partial<Device> = {}): Device =>
  ({
    id: 'n1',
    name: 'box',
    dnsName: 'box.tail.ts.net',
    hostname: 'box',
    os: 'linux',
    addresses: ['100.64.0.1'],
    user: 'a@example.com',
    tags: [],
    isSelf: false,
    isExternal: false,
    online: true,
    active: true,
    lastSeen: '2026-01-01T00:00:00Z',
    created: '2026-01-01T00:00:00Z',
    firstSeen: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z',
    updateAvailable: false,
    authorized: true,
    keyExpiryDisabled: false,
    expired: false,
    exitNodeOption: false,
    isExitNode: false,
    advertisedRoutes: [],
    enabledRoutes: [],
    primaryRoutes: [],
    blocksIncomingConnections: false,
    connectivity: { path: 'direct', rxBytes: 0, txBytes: 0, rxRate: 0, txRate: 0 },
    agent: { state: 'unknown' },
    uptime: {},
    ...over,
  }) as Device

describe('pingResultToast', () => {
  const dev = { id: 'n1', name: 'alice-mbp' }
  it('keys the toast per device and summarises a successful ping', () => {
    const t = pingResultToast(dev, { deviceId: 'n1', ip: '100.64.0.4', latencyMs: 12.3, path: 'relay', relay: 'fra', endpoint: '1.2.3.4:41641', at: '2026-09-28T12:00:00Z' })
    expect(t.id).toBe('ping-n1')
    expect(t.tone).toBe('success')
    expect(t.title).toBe('Ping · alice-mbp')
    expect(t.description).toContain('1.2.3.4:41641')
    expect(t.description).toContain('fra')
  })
  it('reports failures with the error text', () => {
    const t = pingResultToast(dev, { deviceId: 'n1', ip: '100.64.0.4', latencyMs: 0, path: 'unknown', error: 'timeout', at: '2026-09-28T12:00:00Z' })
    expect(t).toMatchObject({ id: 'ping-n1', tone: 'error', description: 'timeout' })
  })
})

describe('tag validation', () => {
  it('normalises input to tag:name', () => {
    expect(normalizeTag('  Server ')).toBe('tag:server')
    expect(normalizeTag('tag:nas')).toBe('tag:nas')
    expect(normalizeTag('')).toBe('')
  })
  it('rejects bad tags and duplicates', () => {
    expect(validateTag('')).toMatch(/enter/i)
    expect(validateTag('tag:has space')).toMatch(/tag:name/)
    expect(validateTag('tag:ok', ['tag:ok'])).toMatch(/already/)
    expect(validateTag('tag:ok-1', ['tag:other'])).toBeNull()
  })
})

describe('validateDeviceName', () => {
  it('accepts DNS labels only', () => {
    expect(validateDeviceName('nas-01')).toBeNull()
    expect(validateDeviceName('NAS')).toBeNull()
    expect(validateDeviceName('')).toMatch(/enter/i)
    expect(validateDeviceName('-bad')).toMatch(/dashes/)
    expect(validateDeviceName('bad_name')).toMatch(/dashes/)
    expect(validateDeviceName('a'.repeat(64))).toMatch(/63/)
    expect(validateDeviceName('same', 'same')).toMatch(/already/)
  })
})

const points: SeriesPoint[] = [
  { t: 100, online: 1, latencyMs: 5, tsRxRate: 10, tsTxRate: 1, cpu: 20 },
  { t: 160, online: 0, tsRxRate: 0, tsTxRate: 0 },
  { t: 220, online: 1, latencyMs: 7, tsRxRate: 12, tsTxRate: 2, cpu: 30 },
]

describe('series helpers', () => {
  it('maps points to rows keyed by API field', () => {
    const rows = seriesRows(points)
    expect(rows[0]).toMatchObject({ t: 100, latencyMs: 5, cpu: 20 })
    expect(rows[1]!.latencyMs).toBeUndefined()
  })
  it('detects present fields', () => {
    expect(seriesHas(points, 'cpu')).toBe(true)
    expect(seriesHas(points, 'tempC')).toBe(false)
  })
  it('trend keeps nulls for gaps and caps the length', () => {
    expect(trend(points, 'latencyMs')).toEqual([5, null, 7])
    const many = Array.from({ length: 200 }, (_, i) => ({ t: i, online: 1, tsRxRate: i, tsTxRate: 0 }))
    const t = trend(many, 'tsRxRate', 48)
    expect(t.length).toBeLessThanOrEqual(48)
    expect(t[t.length - 1]).toBe(199)
    expect(trend([], 'cpu')).toEqual([])
  })
})

describe('derpRows', () => {
  it('sorts by latency and names regions', () => {
    const rows = derpRows({ fra: 90, nyc: 12, sfo: 30 }, 'nyc', { nyc: 'New York City' })
    expect(rows.map((r) => r.code)).toEqual(['nyc', 'sfo', 'fra'])
    expect(rows[0]).toMatchObject({ name: 'New York City', preferred: true })
    expect(rows[1]!.name).toBe('sfo')
    expect(derpRows(undefined, undefined)).toEqual([])
  })
})

describe('natRows', () => {
  it('reports null when nothing is known', () => {
    const rows = natRows(undefined, undefined)
    expect(rows.every((r) => r.supported === null)).toBe(true)
  })
  it('flags mapping variance as bad-when-true', () => {
    const rows = natRows({ hairPinning: true, ipv6: false, pcp: false, pmp: true, udp: true, upnp: false }, true)
    expect(rows.find((r) => r.id === 'udp')?.supported).toBe(true)
    expect(rows.find((r) => r.id === 'mapping')).toMatchObject({ supported: true, goodWhenTrue: false })
  })
})

describe('routeRows', () => {
  it('merges advertised and enabled, exit routes last', () => {
    const rows = routeRows({ advertisedRoutes: ['0.0.0.0/0', '10.0.0.0/24', '192.168.1.0/24'], enabledRoutes: ['10.0.0.0/24', '172.16.0.0/12'], primaryRoutes: ['10.0.0.0/24'] })
    expect(rows.map((r) => r.route)).toEqual(['10.0.0.0/24', '172.16.0.0/12', '192.168.1.0/24', '0.0.0.0/0'])
    expect(rows[0]).toMatchObject({ enabled: true, primary: true, exit: false })
    expect(rows[3]).toMatchObject({ enabled: false, exit: true })
    expect(isExitRoute('::/0')).toBe(true)
  })
})

const report: UptimeReport = {
  deviceId: 'n1',
  from: '2026-01-01T00:00:00Z',
  to: '2026-01-01T10:00:00Z',
  pct: 80,
  outages: 1,
  segments: [
    { from: '2026-01-01T00:00:00Z', to: '2026-01-01T04:00:00Z', online: true },
    { from: '2026-01-01T04:00:00Z', to: '2026-01-01T06:00:00Z', online: false },
    { from: '2026-01-01T06:00:00Z', to: '2026-01-01T10:00:00Z', online: true },
  ],
}

describe('timeline', () => {
  it('computes percent geometry', () => {
    const bars = timelineBars(report)
    expect(bars).toHaveLength(3)
    expect(bars[1]).toMatchObject({ online: false, startPct: 40, widthPct: 20, seconds: 7200 })
    expect(timelineBars(undefined)).toEqual([])
    expect(timelineBars({ from: 'x', to: 'y', segments: [] })).toEqual([])
  })
  it('clamps segments to the window', () => {
    const bars = timelineBars({ ...report, segments: [{ from: '2025-12-31T23:00:00Z', to: '2026-01-01T01:00:00Z', online: true }] })
    expect(bars[0]!.startPct).toBe(0)
    expect(bars[0]!.widthPct).toBeCloseTo(10)
  })
  it('lists outages newest first or by length', () => {
    const r2 = { ...report, segments: [...report.segments, { from: '2026-01-01T09:00:00Z', to: '2026-01-01T10:00:00Z', online: false }] }
    const newest = outageList(r2)
    expect(newest[0]).toMatchObject({ seconds: 3600, ongoing: true })
    expect(outageList(r2, true)[0]!.seconds).toBe(7200)
  })
  it('spreads ticks across the window', () => {
    const ticks = timelineTicks(report.from, report.to, 3)
    expect(ticks).toHaveLength(3)
    expect(ticks[1]).toBe(new Date('2026-01-01T05:00:00Z').getTime())
    expect(timelineTicks('bad', report.to)).toEqual([])
  })
})

describe('deviceBadges', () => {
  it('derives role badges', () => {
    const d = baseDevice({ isSelf: true, isExitNode: true, enabledRoutes: ['0.0.0.0/0', '10.0.0.0/24'], updateAvailable: true, isExternal: true })
    expect(deviceBadges(d).map((b) => b.id)).toEqual(['self', 'exit', 'router', 'shared', 'update'])
  })
  it('flags pending routes and leaves unauthorized to the headline status', () => {
    const d = baseDevice({ authorized: false, advertisedRoutes: ['10.0.0.0/24'], exitNodeOption: true })
    expect(deviceBadges(d).map((b) => b.id)).toEqual(['exit-offered', 'router-pending'])
  })
})

describe('misc', () => {
  it('builds the install command with a custom port', () => {
    expect(agentInstallCommand(41820)).not.toContain('--port')
    expect(agentInstallCommand(5000)).toContain('--port 5000')
  })
  it('loadPercent scales by cores', () => {
    expect(loadPercent(2, 4)).toBe(50)
    expect(loadPercent(undefined, 4)).toBeNull()
    expect(loadPercent(1, 0)).toBeNull()
  })
  it('interfaceRows puts the tailscale interface first', () => {
    const rows = interfaceRows({
      tailscaleInterface: 'tailscale0',
      interfaces: [
        { name: 'eth0', rxBytes: 0, txBytes: 0, rxPackets: 0, txPackets: 0, rxErrors: 0, txErrors: 0, rxRate: 100, txRate: 100 },
        { name: 'tailscale0', rxBytes: 0, txBytes: 0, rxPackets: 0, txPackets: 0, rxErrors: 0, txErrors: 0, rxRate: 1, txRate: 1 },
      ],
    })
    expect(rows[0]).toMatchObject({ name: 'tailscale0', tailscale: true })
  })
})
