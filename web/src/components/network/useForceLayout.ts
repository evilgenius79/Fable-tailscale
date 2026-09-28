import { useEffect, useMemo, useRef, useState } from 'react'
import { forceCollide, forceLink, forceManyBody, forceRadial, forceSimulation, forceX, forceY, type Simulation, type SimulationLinkDatum, type SimulationNodeDatum } from 'd3-force'
import type { TopologyEdge, TopologyNode } from '../../api/types'
import { ringRadius } from './topology'

export interface LayoutNode extends SimulationNodeDatum {
  id: string
  node: TopologyNode
  /** Glyph radius (px, world units). */
  r: number
  x: number
  y: number
}

export interface LayoutEdge extends SimulationLinkDatum<LayoutNode> {
  id: string
  edge: TopologyEdge
  source: LayoutNode
  target: LayoutNode
}

export interface ForceLayout {
  nodes: LayoutNode[]
  edges: LayoutEdge[]
  /** Increments on every rendered tick; use it to memoize derived geometry. */
  version: number
  /** True once the simulation has cooled (or immediately under reduced motion). */
  settled: boolean
  /** Ring base radius in world units (for the background guides). */
  baseRadius: number
  /** Warm the simulation back up (after a re-layout request). */
  reheat: () => void
}

export const HUB_RADIUS = 20
export const NODE_RADIUS = 13

/** Deterministic angle jitter so first paint is not a perfect polygon. */
function jitter(id: string): number {
  let h = 0
  for (let i = 0; i < id.length; i++) h = (h * 31 + id.charCodeAt(i)) | 0
  return ((h % 1000) / 1000 - 0.5) * 0.4
}

/**
 * d3-force layout: the hub is pinned at the origin, peers are pulled toward
 * concentric rings by path (direct → relay → offline), links keep neighbours
 * close and collision keeps glyphs apart. Positions persist across data
 * refreshes so the map does not jump when a rate or latency changes; the
 * simulation only restarts when the set of nodes/edges or the viewport
 * changes, cools after ~2s and then stops ticking entirely. Renders are
 * coalesced through requestAnimationFrame.
 */
export function useForceLayout(nodes: ReadonlyArray<TopologyNode>, edges: ReadonlyArray<TopologyEdge>, width: number, height: number, reducedMotion: boolean): ForceLayout {
  const pool = useRef(new Map<string, LayoutNode>())
  const simRef = useRef<Simulation<LayoutNode, LayoutEdge> | null>(null)
  const [version, setVersion] = useState(0)
  const [settled, setSettled] = useState(false)
  const baseRadius = Math.max(120, Math.min(width, height) * 0.3)

  // Stable identity keys: restart only when membership changes.
  const nodeKey = useMemo(() => nodes.map((n) => `${n.id}:${n.online ? 1 : 0}:${n.path}`).join('|'), [nodes])
  const edgeKey = useMemo(() => edges.map((e) => `${e.from}>${e.to}`).join('|'), [edges])

  // Membership (create/drop LayoutNode objects) only changes with nodeKey, so a
  // data refresh that keeps the same devices never restarts the simulation.
  const layoutNodes = useMemo(() => {
    const seen = new Set<string>()
    const out: LayoutNode[] = []
    // Seed new nodes evenly around their own ring (direct / relay / offline),
    // staggering the rings so neighbours on different rings do not line up.
    const groups = new Map<number, TopologyNode[]>()
    for (const n of nodes) {
      if (n.isSelf) continue
      const rr = ringRadius(n, baseRadius)
      groups.set(rr, [...(groups.get(rr) ?? []), n])
    }
    const ringIndex = Array.from(groups.keys()).sort((a, b) => a - b)
    nodes.forEach((n) => {
      seen.add(n.id)
      let ln = pool.current.get(n.id)
      const r = n.isSelf ? HUB_RADIUS : NODE_RADIUS
      if (!ln) {
        const rr = ringRadius(n, baseRadius)
        const group = groups.get(rr) ?? []
        const i = group.indexOf(n)
        const stagger = (ringIndex.indexOf(rr) * Math.PI) / 5
        const angle = n.isSelf ? 0 : (i / Math.max(1, group.length)) * Math.PI * 2 - Math.PI / 2 + stagger + jitter(n.id)
        ln = { id: n.id, node: n, r, x: Math.cos(angle) * rr, y: Math.sin(angle) * rr }
        pool.current.set(n.id, ln)
      }
      ln.node = n
      ln.r = r
      if (n.isSelf) {
        ln.fx = 0
        ln.fy = 0
        ln.x = 0
        ln.y = 0
      } else {
        ln.fx = undefined
        ln.fy = undefined
      }
      out.push(ln)
    })
    for (const id of Array.from(pool.current.keys())) if (!seen.has(id)) pool.current.delete(id)
    return out
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [nodeKey, baseRadius])

  // Keep the TopologyNode payload fresh (latency, rates, relay) without touching identity.
  useMemo(() => {
    for (const n of nodes) {
      const ln = pool.current.get(n.id)
      if (ln) ln.node = n
    }
  }, [nodes])

  const layoutEdges = useMemo(() => {
    const byId = new Map(layoutNodes.map((n) => [n.id, n]))
    const out: LayoutEdge[] = []
    for (const e of edges) {
      const s = byId.get(e.from)
      const t = byId.get(e.to)
      if (s && t) out.push({ id: `${e.from}>${e.to}`, edge: e, source: s, target: t })
    }
    return out
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [layoutNodes, edgeKey])

  // Same for edge payloads (rates change every poll).
  useMemo(() => {
    const byKey = new Map(edges.map((e) => [`${e.from}>${e.to}`, e]))
    for (const le of layoutEdges) {
      const e = byKey.get(le.id)
      if (e) le.edge = e
    }
  }, [edges, layoutEdges])

  useEffect(() => {
    if (!layoutNodes.length || width <= 0 || height <= 0) return
    setSettled(false)
    const sim = forceSimulation<LayoutNode, LayoutEdge>(layoutNodes)
      .force(
        'link',
        forceLink<LayoutNode, LayoutEdge>(layoutEdges)
          .id((d) => d.id)
          .distance((l) => ringRadius(l.target.node, baseRadius))
          .strength(0.2),
      )
      .force('charge', forceManyBody<LayoutNode>().strength(-120).distanceMax(baseRadius * 2.2))
      .force('radial', forceRadial<LayoutNode>((d) => ringRadius(d.node, baseRadius), 0, 0).strength(0.85))
      // Collision radius leaves room for the name label under each glyph.
      .force('collide', forceCollide<LayoutNode>((d) => d.r + (layoutNodes.length <= 60 ? 30 : 12)).strength(1).iterations(3))
      .force('x', forceX<LayoutNode>(0).strength(0.02))
      .force('y', forceY<LayoutNode>(0).strength(0.02))
      .alpha(1)
      .alphaMin(0.015)
      .alphaDecay(reducedMotion ? 0.05 : 0.028)
      .velocityDecay(0.35)
    simRef.current = sim

    let raf = 0
    const flush = () => {
      raf = 0
      setVersion((v) => v + 1)
    }
    if (reducedMotion) {
      sim.stop()
      sim.tick(220)
      flush()
      setSettled(true)
    } else {
      sim.on('tick', () => {
        if (!raf) raf = requestAnimationFrame(flush)
      })
      sim.on('end', () => {
        if (!raf) raf = requestAnimationFrame(flush)
        setSettled(true)
      })
    }
    return () => {
      sim.stop()
      sim.on('tick', null).on('end', null)
      if (raf) cancelAnimationFrame(raf)
      simRef.current = null
    }
  }, [layoutNodes, layoutEdges, width, height, baseRadius, reducedMotion])

  const reheat = () => {
    const sim = simRef.current
    if (!sim) return
    setSettled(false)
    if (reducedMotion) {
      sim.alpha(0.6).stop()
      sim.tick(150)
      setVersion((v) => v + 1)
      setSettled(true)
    } else {
      sim.alpha(0.6).restart()
    }
  }

  return { nodes: layoutNodes, edges: layoutEdges, version, settled, baseRadius, reheat }
}

/** Live `prefers-reduced-motion` flag. */
export function usePrefersReducedMotion(): boolean {
  const [reduced, setReduced] = useState(() => (typeof window !== 'undefined' && typeof window.matchMedia === 'function' ? window.matchMedia('(prefers-reduced-motion: reduce)').matches : false))
  useEffect(() => {
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return
    const mq = window.matchMedia('(prefers-reduced-motion: reduce)')
    const on = () => setReduced(mq.matches)
    mq.addEventListener('change', on)
    return () => mq.removeEventListener('change', on)
  }, [])
  return reduced
}

/** Observe an element's content box (width and height). */
export function useElementSize<T extends HTMLElement>(): [React.RefObject<T | null>, { width: number; height: number }] {
  const ref = useRef<T | null>(null)
  const [size, setSize] = useState({ width: 0, height: 0 })
  useEffect(() => {
    const el = ref.current
    if (!el) return
    const r = el.getBoundingClientRect()
    setSize({ width: r.width, height: r.height })
    if (typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver((entries) => {
      const e = entries[0]
      if (e) setSize({ width: e.contentRect.width, height: e.contentRect.height })
    })
    ro.observe(el)
    return () => ro.disconnect()
  }, [])
  return [ref, size]
}
