// Pure helpers for the Network page: topology filtering, DERP aggregation,
// path summary, latency distribution, ranking and the map's geometry scales.
// No React; unit-tested.

import type { Device, PathType, Topology, TopologyEdge, TopologyNode } from '../../api/types'

// ---------------------------------------------------------------------------
// Filters
// ---------------------------------------------------------------------------

export type PathFilter = 'all' | 'direct' | 'relay'

export interface TopologyFilters {
  onlineOnly: boolean
  path: PathFilter
}

export const DEFAULT_TOPOLOGY_FILTERS: TopologyFilters = { onlineOnly: false, path: 'all' }

/** Apply the map filters; the hub node is always kept and edges need both endpoints. */
export function filterTopology(topo: Pick<Topology, 'nodes' | 'edges'>, f: TopologyFilters): { nodes: TopologyNode[]; edges: TopologyEdge[] } {
  const nodes = topo.nodes.filter((n) => {
    if (n.isSelf) return true
    if (f.onlineOnly && !n.online) return false
    if (f.path === 'direct' && n.path !== 'direct') return false
    if (f.path === 'relay' && n.path !== 'relay') return false
    return true
  })
  const ids = new Set(nodes.map((n) => n.id))
  const edges = topo.edges.filter((e) => ids.has(e.from) && ids.has(e.to) && e.from !== e.to)
  return { nodes, edges }
}

// ---------------------------------------------------------------------------
// Aggregations
// ---------------------------------------------------------------------------

export interface DerpRegionStat {
  code: string
  name: string
  /** Online devices currently relayed through this region. */
  inUse: number
  /** Devices whose preferred (home) DERP is this region. */
  preferred: number
  /** Mean of the per-device DERP latency probes (null when none reported). */
  avgLatencyMs: number | null
  minLatencyMs: number | null
  samples: number
}

/**
 * DERP usage per region: relay counts come from the topology nodes, latency
 * probes and home-region counts from each device's connectivity report.
 */
export function aggregateDerp(devices: ReadonlyArray<Device>, nodes: ReadonlyArray<TopologyNode>, regions: Record<string, string> = {}): DerpRegionStat[] {
  const stats = new Map<string, DerpRegionStat>()
  const get = (code: string) => {
    let s = stats.get(code)
    if (!s) {
      s = { code, name: regions[code] ?? code, inUse: 0, preferred: 0, avgLatencyMs: null, minLatencyMs: null, samples: 0 }
      stats.set(code, s)
    }
    return s
  }
  const sums = new Map<string, number>()
  for (const n of nodes) {
    if (n.online && n.path === 'relay' && n.relay) get(n.relay).inUse++
  }
  for (const d of devices) {
    const c = d.connectivity
    if (c.preferredDerp) get(c.preferredDerp).preferred++
    for (const [code, v] of Object.entries(c.derpLatencyMs ?? {})) {
      if (typeof v !== 'number' || !Number.isFinite(v) || v < 0) continue
      const s = get(code)
      s.samples++
      sums.set(code, (sums.get(code) ?? 0) + v)
      s.minLatencyMs = s.minLatencyMs === null ? v : Math.min(s.minLatencyMs, v)
    }
  }
  for (const s of stats.values()) {
    if (s.samples) s.avgLatencyMs = (sums.get(s.code) ?? 0) / s.samples
  }
  return Array.from(stats.values()).sort(
    (a, b) => b.inUse - a.inUse || b.preferred - a.preferred || (a.avgLatencyMs ?? Infinity) - (b.avgLatencyMs ?? Infinity) || a.code.localeCompare(b.code),
  )
}

export interface PathSummary {
  direct: number
  relay: number
  /** Online but no path known yet. */
  unknown: number
  offline: number
  online: number
  total: number
}

/** Counts of online nodes by path plus offline nodes. */
export function pathSummary(nodes: ReadonlyArray<TopologyNode>): PathSummary {
  const s: PathSummary = { direct: 0, relay: 0, unknown: 0, offline: 0, online: 0, total: nodes.length }
  for (const n of nodes) {
    if (!n.online) {
      s.offline++
      continue
    }
    s.online++
    if (n.path === 'direct') s.direct++
    else if (n.path === 'relay') s.relay++
    else s.unknown++
  }
  return s
}

export interface LatencyBucket {
  label: string
  /** Inclusive lower bound (ms). */
  min: number
  /** Exclusive upper bound (ms); Infinity for the last bucket. */
  max: number
  count: number
}

export const LATENCY_BUCKETS: ReadonlyArray<Pick<LatencyBucket, 'label' | 'min' | 'max'>> = [
  { label: '< 5 ms', min: 0, max: 5 },
  { label: '5–20 ms', min: 5, max: 20 },
  { label: '20–50 ms', min: 20, max: 50 },
  { label: '50–100 ms', min: 50, max: 100 },
  { label: '100–250 ms', min: 100, max: 250 },
  { label: '≥ 250 ms', min: 250, max: Infinity },
]

/** Histogram of latencies over the fixed, ordered buckets (every bucket is returned, even when empty). */
export function latencyHistogram(values: ReadonlyArray<number | null | undefined>): LatencyBucket[] {
  const out = LATENCY_BUCKETS.map((b) => ({ ...b, count: 0 }))
  for (const v of values) {
    if (typeof v !== 'number' || !Number.isFinite(v) || v < 0) continue
    const b = out.find((x) => v >= x.min && v < x.max)
    if (b) b.count++
  }
  return out
}

export interface LatencyStats {
  count: number
  avg: number | null
  p50: number | null
  p95: number | null
  max: number | null
}

function quantile(sorted: number[], q: number): number | null {
  if (!sorted.length) return null
  const pos = (sorted.length - 1) * q
  const lo = Math.floor(pos)
  const hi = Math.ceil(pos)
  const a = sorted[lo]!
  const b = sorted[hi]!
  return a + (b - a) * (pos - lo)
}

/** Summary statistics for a set of latencies. */
export function latencyStats(values: ReadonlyArray<number | null | undefined>): LatencyStats {
  const v = values.filter((x): x is number => typeof x === 'number' && Number.isFinite(x) && x >= 0).sort((a, b) => a - b)
  if (!v.length) return { count: 0, avg: null, p50: null, p95: null, max: null }
  return { count: v.length, avg: v.reduce((a, b) => a + b, 0) / v.length, p50: quantile(v, 0.5), p95: quantile(v, 0.95), max: v[v.length - 1]! }
}

/** Online peers with a latency, slowest first (ties broken by name). */
export function slowestLinks(nodes: ReadonlyArray<TopologyNode>, limit = 5): TopologyNode[] {
  return nodes
    .filter((n) => n.online && !n.isSelf && typeof n.latencyMs === 'number' && Number.isFinite(n.latencyMs))
    .sort((a, b) => b.latencyMs! - a.latencyMs! || a.name.localeCompare(b.name))
    .slice(0, limit)
}

// ---------------------------------------------------------------------------
// Map geometry
// ---------------------------------------------------------------------------

/** Edge stroke width from throughput: sqrt scale into [1, 5] px, relative to the busiest edge. */
export function edgeWidth(bytesPerSec: number, maxBytesPerSec: number, min = 1, max = 5): number {
  if (!(maxBytesPerSec > 0) || !(bytesPerSec > 0)) return min
  const r = Math.sqrt(Math.min(bytesPerSec, maxBytesPerSec) / maxBytesPerSec)
  return Math.round((min + (max - min) * r) * 10) / 10
}

/** Ring the node is pulled toward, relative to a base radius: direct peers close, relayed further, offline outermost. */
export function ringRadius(node: Pick<TopologyNode, 'online' | 'path' | 'isSelf'>, base: number): number {
  if (node.isSelf) return 0
  if (!node.online) return base * 1.75
  if (node.path === 'direct') return base
  if (node.path === 'relay') return base * 1.4
  return base * 1.6
}

export interface Transform {
  k: number
  x: number
  y: number
}

/** Zoom/pan transform that fits a point cloud into a viewport with padding. */
export function fitTransform(points: ReadonlyArray<{ x: number; y: number }>, width: number, height: number, padding = 40, minK = 0.25, maxK = 1.6): Transform {
  if (!points.length || width <= 0 || height <= 0) return { k: 1, x: width / 2, y: height / 2 }
  let minX = Infinity
  let minY = Infinity
  let maxX = -Infinity
  let maxY = -Infinity
  for (const p of points) {
    if (p.x < minX) minX = p.x
    if (p.y < minY) minY = p.y
    if (p.x > maxX) maxX = p.x
    if (p.y > maxY) maxY = p.y
  }
  const w = Math.max(1, maxX - minX)
  const h = Math.max(1, maxY - minY)
  const k = Math.max(minK, Math.min(maxK, Math.min((width - padding * 2) / w, (height - padding * 2) / h)))
  const cx = (minX + maxX) / 2
  const cy = (minY + maxY) / 2
  return { k, x: width / 2 - cx * k, y: height / 2 - cy * k }
}

/** Clamp a zoom factor to the supported range. */
export function clampZoom(k: number, minK = 0.25, maxK = 4): number {
  return Math.max(minK, Math.min(maxK, k))
}

/** Zoom about a viewport point so the point under the cursor stays put. */
export function zoomAt(t: Transform, factor: number, px: number, py: number, minK = 0.25, maxK = 4): Transform {
  const k = clampZoom(t.k * factor, minK, maxK)
  const ratio = k / t.k
  return { k, x: px - (px - t.x) * ratio, y: py - (py - t.y) * ratio }
}

/** Human label for an edge's path. */
export function edgePathLabel(path: PathType, relay?: string): string {
  if (path === 'direct') return 'Direct'
  if (path === 'relay') return relay ? `Relay via ${relay}` : 'Relay'
  if (path === 'none') return 'No path'
  return 'Unknown path'
}
