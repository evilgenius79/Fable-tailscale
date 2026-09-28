import { describe, expect, it } from 'vitest'
import { PROFILES, advance, buildSeries, buildUptime, createMockState, findDevice, overview } from './mockData'

const NOW = Date.parse('2026-09-28T15:00:00Z')

describe('mock fixtures', () => {
  it('builds the demo fleet with the documented states', () => {
    const s = createMockState(NOW)
    expect(s.devices.length).toBeGreaterThanOrEqual(16)
    const byName = Object.fromEntries(s.devices.map((d) => [d.name, d]))
    expect(byName['netwatch-hub']!.isSelf).toBe(true)
    expect(byName['new-laptop-7f2a']!.authorized).toBe(false)
    expect(byName['alice-iphone']!.online).toBe(false)
    expect(byName['contractor-laptop']!.isExternal).toBe(true)
    expect(byName['edge-exit-1']!.isExitNode).toBe(true)
    expect(byName['nas-basement']!.metrics!.diskPercent).toBeGreaterThan(90)
    expect(byName['edge-exit-1']!.metrics!.memPercent).toBeGreaterThan(90)
    expect(byName['bob-thinkpad']!.agent.state).toBe('unreachable')
    expect(byName['pi-garage']!.connectivity.path).toBe('relay')
    expect(byName['backup-server']!.updateAvailable).toBe(true)
    const days = (Date.parse(byName['media-box']!.keyExpiry!) - NOW) / 86400000
    expect(days).toBeCloseTo(3, 0)
    expect(s.alerts.filter((a) => a.state === 'open').length).toBeGreaterThanOrEqual(5)
    expect(s.events.length).toBeGreaterThanOrEqual(40)
    expect(s.rules.length).toBe(13)
    expect(s.identity.role).toBe('admin')
  })

  it('series are deterministic, cover the range and have gaps when offline', () => {
    const pi = PROFILES.find((p) => p.name === 'pi-garage')!
    const a = buildSeries(pi, '24h', NOW)
    const b = buildSeries(pi, '24h', NOW)
    expect(a).toEqual(b)
    expect(a.stepSeconds).toBe(60)
    expect(a.points.length).toBeGreaterThanOrEqual(1440)
    expect(a.points.every((p) => typeof p.cpu === 'number')).toBe(true)
    const phone = PROFILES.find((p) => p.name === 'alice-iphone')!
    const ps = buildSeries(phone, '6h', NOW)
    expect(ps.points[ps.points.length - 1]!.online).toBe(0)
    expect(ps.points[ps.points.length - 1]!.cpu).toBeUndefined()
    expect(ps.points.some((p) => p.online === 1)).toBe(true)
    const flap = PROFILES.find((p) => p.name === 'backup-server')!
    const up = buildUptime(flap, '24h', NOW)
    expect(up.outages).toBeGreaterThan(20)
    expect(up.pct).toBeLessThan(95)
  })

  it('overview counts agree with the device list', () => {
    const s = createMockState(NOW)
    const ov = overview(s, NOW)
    expect(ov.devices).toBe(s.devices.length)
    expect(ov.online + ov.offline).toBe(ov.devices)
    expect(ov.unauthorized).toBe(1)
    expect(ov.exitNodes).toBe(1)
    expect(ov.sparklines.t.length).toBe(96)
    expect(ov.sparklines.online.length).toBe(96)
    expect(Object.values(ov.osBreakdown).reduce((x, y) => x + y, 0)).toBe(ov.devices)
  })

  it('advance keeps state consistent and applies overrides', () => {
    const s = createMockState(NOW)
    const d = findDevice(s, 'nas-basement')!
    s.overrides.set(d.id, { name: 'renamed' })
    const r = advance(s, NOW + 5000)
    expect(r.devices.find((x) => x.id === d.id)!.name).toBe('renamed')
    expect(findDevice(s, 'nas-basement')!.name).toBe('renamed')
    expect(r.overview.devices).toBe(s.devices.length)
  })
})
