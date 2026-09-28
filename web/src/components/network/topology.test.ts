import { describe, expect, it } from 'vitest'
import type { Device, TopologyEdge, TopologyNode } from '../../api/types'
import {
  aggregateDerp,
  clampZoom,
  edgePathLabel,
  edgeWidth,
  filterTopology,
  fitTransform,
  latencyHistogram,
  latencyStats,
  pathSummary,
  ringRadius,
  slowestLinks,
  zoomAt,
} from './topology'

const node = (over: Partial<TopologyNode>): TopologyNode => ({
  id: over.id ?? 'x',
  name: over.name ?? over.id ?? 'x',
  os: 'linux',
  online: true,
  isSelf: false,
  isExitNode: false,
  isSubnetRouter: false,
  tags: [],
  user: 'a',
  path: 'direct',
  ...over,
})

const hub = node({ id: 'hub', isSelf: true })
const nodes: TopologyNode[] = [
  hub,
  node({ id: 'a', path: 'direct', latencyMs: 2 }),
  node({ id: 'b', path: 'relay', relay: 'nyc', latencyMs: 40 }),
  node({ id: 'c', path: 'relay', relay: 'nyc', latencyMs: 55 }),
  node({ id: 'd', online: false, path: 'none' }),
  node({ id: 'e', path: 'unknown', latencyMs: 300 }),
]
const edges: TopologyEdge[] = [
  { from: 'hub', to: 'a', path: 'direct', rxRate: 100, txRate: 50 },
  { from: 'hub', to: 'b', path: 'relay', relay: 'nyc', rxRate: 10, txRate: 5 },
  { from: 'hub', to: 'c', path: 'relay', relay: 'nyc', rxRate: 0, txRate: 0 },
  { from: 'hub', to: 'e', path: 'unknown', rxRate: 0, txRate: 0 },
]

describe('filterTopology', () => {
  it('keeps everything by default', () => {
    const r = filterTopology({ nodes, edges }, { onlineOnly: false, path: 'all' })
    expect(r.nodes).toHaveLength(6)
    expect(r.edges).toHaveLength(4)
  })
  it('drops offline nodes but never the hub', () => {
    const r = filterTopology({ nodes: [node({ id: 'hub', isSelf: true, online: false }), ...nodes.slice(1)], edges }, { onlineOnly: true, path: 'all' })
    expect(r.nodes.map((n) => n.id)).toEqual(['hub', 'a', 'b', 'c', 'e'])
  })
  it('filters by path and prunes dangling edges', () => {
    const r = filterTopology({ nodes, edges }, { onlineOnly: false, path: 'relay' })
    expect(r.nodes.map((n) => n.id)).toEqual(['hub', 'b', 'c'])
    expect(r.edges.map((e) => e.to)).toEqual(['b', 'c'])
  })
})

const device = (id: string, c: Partial<Device['connectivity']>): Device =>
  ({ id, name: id, connectivity: { path: 'direct', rxBytes: 0, txBytes: 0, rxRate: 0, txRate: 0, ...c } }) as Device

describe('aggregateDerp', () => {
  it('counts relay usage, home regions and averages probes', () => {
    const devices = [
      device('a', { derpLatencyMs: { nyc: 10, fra: 90 }, preferredDerp: 'nyc' }),
      device('b', { derpLatencyMs: { nyc: 20, fra: 100, sfo: 30 }, preferredDerp: 'nyc' }),
      device('c', { derpLatencyMs: { nyc: 30 }, preferredDerp: 'sfo' }),
    ]
    const stats = aggregateDerp(devices, nodes, { nyc: 'New York City' })
    expect(stats[0]).toMatchObject({ code: 'nyc', name: 'New York City', inUse: 2, preferred: 2, avgLatencyMs: 20, minLatencyMs: 10, samples: 3 })
    const sfo = stats.find((s) => s.code === 'sfo')
    expect(sfo).toMatchObject({ inUse: 0, preferred: 1, avgLatencyMs: 30, samples: 1 })
    const fra = stats.find((s) => s.code === 'fra')
    expect(fra?.avgLatencyMs).toBe(95)
    // sfo (preferred 1) sorts before fra (preferred 0)
    expect(stats.map((s) => s.code)).toEqual(['nyc', 'sfo', 'fra'])
  })
  it('ignores invalid probes and returns an empty list without data', () => {
    expect(aggregateDerp([device('a', { derpLatencyMs: { nyc: Number.NaN } })], [], {})).toEqual([])
    expect(aggregateDerp([], [], {})).toEqual([])
  })
})

describe('pathSummary', () => {
  it('counts by path', () => {
    expect(pathSummary(nodes)).toEqual({ direct: 2, relay: 2, unknown: 1, offline: 1, online: 5, total: 6 })
  })
})

describe('latency', () => {
  it('buckets values into ordered ranges', () => {
    const h = latencyHistogram([1, 4.9, 5, 19, 20, 49, 120, 250, 999, null, -1, Number.NaN])
    expect(h.map((b) => b.count)).toEqual([2, 2, 2, 0, 1, 2])
    expect(h[0]!.label).toBe('< 5 ms')
  })
  it('summarises statistics', () => {
    const s = latencyStats([10, 20, 30, 40, null])
    expect(s.count).toBe(4)
    expect(s.avg).toBe(25)
    expect(s.p50).toBe(25)
    expect(s.max).toBe(40)
    expect(s.p95).toBeCloseTo(38.5)
    expect(latencyStats([])).toEqual({ count: 0, avg: null, p50: null, p95: null, max: null })
  })
  it('ranks the slowest online links', () => {
    expect(slowestLinks(nodes, 2).map((n) => n.id)).toEqual(['e', 'c'])
    expect(slowestLinks(nodes).map((n) => n.id)).toEqual(['e', 'c', 'b', 'a'])
  })
})

describe('geometry', () => {
  it('edgeWidth scales by sqrt into [1,5]', () => {
    expect(edgeWidth(0, 100)).toBe(1)
    expect(edgeWidth(100, 100)).toBe(5)
    expect(edgeWidth(25, 100)).toBe(3)
    expect(edgeWidth(50, 0)).toBe(1)
  })
  it('ringRadius orders rings', () => {
    expect(ringRadius(hub, 100)).toBe(0)
    expect(ringRadius(node({ path: 'direct' }), 100)).toBeLessThan(ringRadius(node({ path: 'relay' }), 100))
    expect(ringRadius(node({ path: 'relay' }), 100)).toBeLessThan(ringRadius(node({ online: false }), 100))
  })
  it('fitTransform centres and scales a point cloud', () => {
    const t = fitTransform(
      [
        { x: -100, y: -50 },
        { x: 100, y: 50 },
      ],
      400,
      300,
      50,
      0.1,
      10,
    )
    expect(t.k).toBeCloseTo(1.5)
    expect(t.x).toBeCloseTo(200)
    expect(t.y).toBeCloseTo(150)
    expect(fitTransform([], 400, 300)).toEqual({ k: 1, x: 200, y: 150 })
  })
  it('zoomAt keeps the cursor point fixed', () => {
    const t = { k: 1, x: 0, y: 0 }
    const z = zoomAt(t, 2, 100, 100)
    // world point under (100,100) was (100,100); after zoom it must still map to (100,100)
    expect(100 * z.k + z.x).toBeCloseTo(100)
    expect(100 * z.k + z.y).toBeCloseTo(100)
    expect(clampZoom(10)).toBe(4)
    expect(clampZoom(0.01)).toBe(0.25)
  })
  it('labels edge paths', () => {
    expect(edgePathLabel('relay', 'fra')).toBe('Relay via fra')
    expect(edgePathLabel('direct')).toBe('Direct')
    expect(edgePathLabel('none')).toBe('No path')
  })
})
