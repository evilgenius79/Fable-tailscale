import { describe, expect, it } from 'vitest'
import { PROFILES, buildAlerts, buildDevice } from '../../api/mockData'
import type { EventType } from '../../api/types'
import { EVENT_ICONS, deltaVsStart, eventIcon, osSlices, percentDeltaVsStart, percentOf, rangeForSpan, sortOpenAlerts, sparklineWindow, sumSeries, topDevices, userItems, windowLabel } from './overview'

const NOW = Date.UTC(2026, 8, 28, 15, 0, 0)
const devices = PROFILES.map((p) => buildDevice(p, NOW))

describe('rangeForSpan / sparklineWindow', () => {
  it('picks the smallest preset covering the span', () => {
    expect(rangeForSpan(3600)).toBe('1h')
    expect(rangeForSpan(3601)).toBe('3h')
    expect(rangeForSpan(95 * 900)).toBe('24h')
    expect(rangeForSpan(10 ** 9)).toBe('30d')
  })
  it('derives span, step and range from the t axis', () => {
    const t = Array.from({ length: 96 }, (_, i) => 1_700_000_000 + i * 900)
    const w = sparklineWindow({ t, online: [], rxRate: [], txRate: [], latency: [] })
    expect(w.span).toBe(95 * 900)
    expect(w.step).toBe(900)
    expect(w.range).toBe('24h')
    expect(sparklineWindow(undefined)).toEqual({ span: 0, step: 0, range: '1h' })
    expect(sparklineWindow({ t: [1], online: [], rxRate: [], txRate: [], latency: [] }).span).toBe(0)
  })
})

describe('windowLabel', () => {
  it('rounds to the hour and uses the compact duration', () => {
    expect(windowLabel(95 * 900)).toBe('vs 1d ago')
    expect(windowLabel(3600)).toBe('vs 1h ago')
    expect(windowLabel(59 * 60 + 40)).toBe('vs 1h ago')
    expect(windowLabel(1500)).toBe('vs 25m ago')
    expect(windowLabel(0)).toBeUndefined()
  })
})

describe('series math', () => {
  it('sums element-wise and nulls the gaps', () => {
    expect(sumSeries([1, 2, 3], [10, 20])).toEqual([11, 22, null])
    expect(sumSeries([], [])).toEqual([])
  })
  it('computes deltas against the first finite value', () => {
    expect(deltaVsStart([10, null, 12, 15])).toBe(5)
    expect(deltaVsStart([7])).toBeNull()
    expect(deltaVsStart([])).toBeNull()
    expect(percentDeltaVsStart([100, 150])).toBe(50)
    expect(percentDeltaVsStart([0, 150])).toBeNull()
    expect(percentDeltaVsStart([200, 100])).toBe(-50)
  })
  it('percentOf guards against an empty fleet', () => {
    expect(percentOf(3, 0)).toBeNull()
    expect(percentOf(1, 4)).toBe(25)
  })
})

describe('topDevices', () => {
  it('ranks online devices by throughput, descending', () => {
    const top = topDevices(devices, 'throughput', 5)
    expect(top).toHaveLength(5)
    expect(top.every((d) => d.online)).toBe(true)
    for (let i = 1; i < top.length; i++) {
      const a = top[i - 1]!.connectivity
      const b = top[i]!.connectivity
      expect(a.rxRate + a.txRate).toBeGreaterThanOrEqual(b.rxRate + b.txRate)
    }
  })
  it('ranks by latency, worst first', () => {
    const top = topDevices(devices, 'latency', 3)
    expect(top[0]!.connectivity.latencyMs).toBeGreaterThanOrEqual(top[1]!.connectivity.latencyMs!)
  })
  it('limits and copes with an empty list', () => {
    expect(topDevices([], 'throughput')).toEqual([])
    expect(topDevices(devices, 'throughput', 2)).toHaveLength(2)
  })
})

describe('breakdowns', () => {
  it('merges os keys into families and sorts by count', () => {
    const slices = osSlices({ linux: 5, Linux: 1, macOS: 2, iOS: 1, android: 1, windows: 3 })
    expect(slices[0]).toEqual({ label: 'Linux', value: 6 })
    expect(slices.find((s) => s.label === 'macOS')?.value).toBe(2)
    expect(osSlices(undefined)).toEqual([])
  })
  it('sorts users by count then name', () => {
    const items = userItems({ 'bob@example.com': 2, 'alice@example.com': 2, 'tagged-devices': 5 })
    expect(items.map((i) => i.label)).toEqual(['tagged-devices', 'alice@example.com', 'bob@example.com'])
  })
})

describe('sortOpenAlerts', () => {
  it('drops resolved, puts unacked and more severe first', () => {
    const sorted = sortOpenAlerts(buildAlerts(NOW))
    expect(sorted.every((a) => a.state === 'open')).toBe(true)
    const firstAcked = sorted.findIndex((a) => !!a.ackedAt)
    if (firstAcked >= 0) expect(sorted.slice(firstAcked).every((a) => !!a.ackedAt)).toBe(true)
    const critical: typeof sorted = [{ ...sorted[0]!, id: 1, severity: 'critical', ackedAt: undefined }]
    expect(sortOpenAlerts([...sorted, ...critical])[0]!.id).toBe(1)
  })
})

describe('event icons', () => {
  it('covers every event type', () => {
    const types: EventType[] = [
      'device.online',
      'device.offline',
      'device.new',
      'device.removed',
      'device.updated',
      'device.authorized',
      'device.expired',
      'device.path_changed',
      'agent.reachable',
      'agent.unreachable',
      'alert.opened',
      'alert.resolved',
      'alert.acked',
      'admin.action',
      'hub.started',
      'hub.error',
    ]
    for (const t of types) expect(EVENT_ICONS[t]).toBeTypeOf('object')
    expect(eventIcon('unknown.type' as EventType)).toBeTypeOf('object')
  })
})
