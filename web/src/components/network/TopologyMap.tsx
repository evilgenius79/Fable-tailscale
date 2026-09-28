import { memo, useCallback, useEffect, useMemo, useRef, useState, type KeyboardEvent as ReactKeyboardEvent, type PointerEvent as ReactPointerEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { Maximize2, Minus, Plus, RefreshCw, Waypoints } from 'lucide-react'
import type { TopologyEdge, TopologyNode } from '../../api/types'
import { cn } from '../../lib/cn'
import { formatBitrate, formatLatency } from '../../lib/format'
import { osInfo } from '../../lib/os'
import { useChartTheme, type ChartTheme } from '../charts'
import { IconButton } from '../ui/Button'
import { EmptyState } from '../ui/EmptyState'
import { ErrorState } from '../ui/ErrorState'
import { Skeleton } from '../ui/Skeleton'
import { edgePathLabel, edgeWidth, fitTransform, ringRadius, zoomAt, type Transform } from './topology'
import { useElementSize, useForceLayout, usePrefersReducedMotion, type LayoutEdge, type LayoutNode } from './useForceLayout'

export interface TopologyMapProps {
  nodes: ReadonlyArray<TopologyNode>
  edges: ReadonlyArray<TopologyEdge>
  loading?: boolean
  fetching?: boolean
  error?: unknown
  onRetry?: () => void
  /** Total number of nodes before filtering (for the empty-after-filter message). */
  unfilteredCount?: number
  className?: string
}

const MIN_K = 0.25
const MAX_K = 4

// ---------------------------------------------------------------------------
// Layers (memoised so pan/zoom only touches the transform attribute)
// ---------------------------------------------------------------------------

interface EdgeLayerProps {
  edges: LayoutEdge[]
  maxRate: number
  theme: ChartTheme
  hovered: string | null
  showLabels: boolean
  version: number
}

const EdgeLayer = memo(function EdgeLayer({ edges, maxRate, theme, hovered, showLabels }: EdgeLayerProps) {
  return (
    <g role="presentation">
      {edges.map((l) => {
        const { edge, source, target } = l
        const w = edgeWidth(edge.rxRate + edge.txRate, maxRate)
        const relay = edge.path === 'relay'
        const none = edge.path === 'none' || edge.path === 'unknown'
        const color = relay ? theme.status.relay : none ? theme.muted : theme.status.direct
        const dim = hovered !== null && hovered !== source.id && hovered !== target.id
        const mx = (source.x + target.x) / 2
        const my = (source.y + target.y) / 2
        return (
          <g key={l.id} opacity={dim ? 0.15 : none ? 0.5 : 1} style={{ transition: 'opacity 150ms' }}>
            <line x1={source.x} y1={source.y} x2={target.x} y2={target.y} stroke={color} strokeWidth={w} strokeDasharray={relay ? '6 5' : none ? '2 4' : undefined} strokeLinecap="round" />
            {relay && edge.relay && (showLabels || hovered === target.id) ? (
              <text
                x={mx}
                y={my}
                dy={3.5}
                textAnchor="middle"
                fontSize={9.5}
                fontFamily={theme.font}
                fontWeight={600}
                fill={theme.status.relay}
                stroke={theme.surface}
                strokeWidth={4}
                paintOrder="stroke"
                style={{ pointerEvents: 'none', textTransform: 'uppercase', letterSpacing: '0.04em' }}
              >
                {edge.relay}
              </text>
            ) : null}
          </g>
        )
      })}
    </g>
  )
})

interface NodeGlyphProps {
  n: LayoutNode
  /** Position passed as primitives so the memo re-renders on every simulation tick. */
  x: number
  y: number
  theme: ChartTheme
  hovered: boolean
  dimmed: boolean
  showLabel: boolean
  onHover: (id: string | null) => void
  onActivate: (id: string) => void
}

const NodeGlyph = memo(function NodeGlyph({ n, x, y, theme, hovered, dimmed, showLabel, onHover, onActivate }: NodeGlyphProps) {
  const d = n.node
  const Icon = osInfo(d.os).icon
  const ringColor = !d.online ? theme.status.offline : d.path === 'relay' ? theme.status.relay : d.path === 'direct' ? theme.status.online : theme.textMuted
  const r = n.r
  const iconSize = d.isSelf ? 20 : 14
  const role = d.isSelf ? 'hub' : d.isExitNode ? 'exit node' : d.isSubnetRouter ? 'subnet router' : null
  const label = `${d.name}${role ? ` (${role})` : ''}: ${d.online ? `online, ${edgePathLabel(d.path, d.relay).toLowerCase()}${typeof d.latencyMs === 'number' ? `, ${formatLatency(d.latencyMs)}` : ''}` : 'offline'}`
  const onKey = (e: ReactKeyboardEvent<SVGGElement>) => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      onActivate(d.id)
    }
  }
  return (
    <g
      transform={`translate(${x},${y})`}
      tabIndex={0}
      role="link"
      aria-label={label}
      data-node-id={d.id}
      className="cursor-pointer outline-none"
      opacity={dimmed ? 0.3 : 1}
      style={{ transition: 'opacity 150ms' }}
      onPointerEnter={() => onHover(d.id)}
      onPointerLeave={() => onHover(null)}
      onFocus={() => onHover(d.id)}
      onBlur={() => onHover(null)}
      onClick={(e) => {
        e.stopPropagation()
        onActivate(d.id)
      }}
      onKeyDown={onKey}
    >
      {/* hit target larger than the glyph */}
      <circle r={r + 10} fill="transparent" />
      {hovered ? <circle r={r + 7} fill="none" stroke={theme.accent} strokeWidth={2} opacity={0.9} /> : null}
      <circle r={r + 3} fill="none" stroke={ringColor} strokeWidth={d.online ? 2 : 1.5} strokeDasharray={d.online ? undefined : '3 3'} opacity={d.online ? 1 : 0.8} />
      <circle r={r} fill={d.isSelf ? theme.accent : theme.surfaceRaised} stroke={d.isSelf ? theme.accent : theme.border} strokeWidth={1} />
      <Icon x={-iconSize / 2} y={-iconSize / 2} width={iconSize} height={iconSize} color={d.isSelf ? '#ffffff' : d.online ? theme.text : theme.textMuted} strokeWidth={1.75} aria-hidden="true" />
      {showLabel || hovered ? (
        <text y={r + 15} textAnchor="middle" fontSize={11} fontFamily={theme.font} fontWeight={d.isSelf ? 600 : 500} fill={d.online ? theme.text : theme.textMuted} stroke={theme.surface} strokeWidth={3.5} paintOrder="stroke" style={{ pointerEvents: 'none' }}>
          {d.name}
        </text>
      ) : null}
      {(showLabel || hovered) && role && !d.isSelf ? (
        <text y={r + 27} textAnchor="middle" fontSize={9.5} fontFamily={theme.font} fill={theme.textMuted} stroke={theme.surface} strokeWidth={3} paintOrder="stroke" style={{ pointerEvents: 'none' }}>
          {role}
        </text>
      ) : null}
    </g>
  )
})

// ---------------------------------------------------------------------------
// Map
// ---------------------------------------------------------------------------

/**
 * Force-directed topology: hub pinned at the centre, peers on concentric rings
 * by path, edges styled by path and weighted by throughput. Wheel/drag/pinch
 * zoom & pan, keyboard-focusable nodes, hover/focus tooltip, click to open.
 */
export function TopologyMap({ nodes, edges, loading, fetching, error, onRetry, unfilteredCount, className }: TopologyMapProps) {
  const theme = useChartTheme()
  const navigate = useNavigate()
  const reduced = usePrefersReducedMotion()
  const [wrapRef, size] = useElementSize<HTMLDivElement>()
  const layout = useForceLayout(nodes, edges, size.width, size.height, reduced)
  const [transform, setTransform] = useState<Transform>({ k: 1, x: 0, y: 0 })
  const [hovered, setHovered] = useState<string | null>(null)
  const interacted = useRef(false)
  const svgRef = useRef<SVGSVGElement>(null)
  const pointers = useRef(new Map<number, { x: number; y: number }>())
  const drag = useRef<{ x: number; y: number; tx: number; ty: number; moved: boolean; pinch?: number } | null>(null)

  const maxRate = useMemo(() => Math.max(0, ...layout.edges.map((e) => e.edge.rxRate + e.edge.txRate)), [layout.edges])

  const fit = useCallback(() => {
    if (!size.width || !size.height) return
    const pts = layout.nodes.map((n) => ({ x: n.x, y: n.y }))
    // Leave room for the zoom controls (right) and the legend (bottom) overlays.
    const t = fitTransform(pts, size.width - 44, size.height - 36, 52, MIN_K, 1.6)
    setTransform({ ...t, y: t.y - 4 })
  }, [layout.nodes, size.width, size.height])

  // Fit once the simulation settles (or on first layout) unless the user has taken control.
  useEffect(() => {
    if (!interacted.current && size.width && size.height) fit()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [layout.settled, size.width, size.height, layout.nodes.length])

  // Native wheel listener (React's is passive, so preventDefault would not work).
  useEffect(() => {
    const el = svgRef.current
    if (!el) return
    const onWheel = (e: WheelEvent) => {
      e.preventDefault()
      interacted.current = true
      const rect = el.getBoundingClientRect()
      const factor = Math.exp(-e.deltaY * (e.deltaMode === 1 ? 0.05 : 0.0015))
      setTransform((t) => zoomAt(t, factor, e.clientX - rect.left, e.clientY - rect.top, MIN_K, MAX_K))
    }
    el.addEventListener('wheel', onWheel, { passive: false })
    return () => el.removeEventListener('wheel', onWheel)
  }, [])

  const onPointerDown = (e: ReactPointerEvent<SVGSVGElement>) => {
    if (e.button !== 0 && e.pointerType === 'mouse') return
    const target = e.target as Element
    if (target.closest('[data-node-id]')) return
    pointers.current.set(e.pointerId, { x: e.clientX, y: e.clientY })
    e.currentTarget.setPointerCapture(e.pointerId)
    if (pointers.current.size === 1) drag.current = { x: e.clientX, y: e.clientY, tx: transform.x, ty: transform.y, moved: false }
    else if (pointers.current.size === 2) {
      const [a, b] = Array.from(pointers.current.values())
      drag.current = { ...(drag.current ?? { x: 0, y: 0, tx: transform.x, ty: transform.y, moved: false }), pinch: Math.hypot(a!.x - b!.x, a!.y - b!.y) }
    }
  }
  const onPointerMove = (e: ReactPointerEvent<SVGSVGElement>) => {
    if (!pointers.current.has(e.pointerId) || !drag.current) return
    pointers.current.set(e.pointerId, { x: e.clientX, y: e.clientY })
    interacted.current = true
    if (pointers.current.size >= 2 && drag.current.pinch) {
      const [a, b] = Array.from(pointers.current.values())
      const dist = Math.hypot(a!.x - b!.x, a!.y - b!.y)
      const factor = dist / drag.current.pinch
      drag.current.pinch = dist
      const rect = e.currentTarget.getBoundingClientRect()
      const cx = (a!.x + b!.x) / 2 - rect.left
      const cy = (a!.y + b!.y) / 2 - rect.top
      setTransform((t) => zoomAt(t, factor, cx, cy, MIN_K, MAX_K))
      return
    }
    const dx = e.clientX - drag.current.x
    const dy = e.clientY - drag.current.y
    if (Math.abs(dx) + Math.abs(dy) > 2) drag.current.moved = true
    const { tx, ty } = drag.current
    setTransform((t) => ({ ...t, x: tx + dx, y: ty + dy }))
  }
  const onPointerUp = (e: ReactPointerEvent<SVGSVGElement>) => {
    pointers.current.delete(e.pointerId)
    if (e.currentTarget.hasPointerCapture(e.pointerId)) e.currentTarget.releasePointerCapture(e.pointerId)
    if (pointers.current.size === 0) drag.current = null
    else if (drag.current) {
      const p = Array.from(pointers.current.values())[0]!
      drag.current = { x: p.x, y: p.y, tx: transform.x, ty: transform.y, moved: true }
    }
  }

  const zoomBy = (f: number) => {
    interacted.current = true
    setTransform((t) => zoomAt(t, f, size.width / 2, size.height / 2, MIN_K, MAX_K))
  }
  const onFit = () => {
    interacted.current = false
    fit()
  }
  const onRelayout = () => {
    interacted.current = false
    layout.reheat()
  }

  const activate = useCallback((id: string) => navigate(`/devices/${encodeURIComponent(id)}`), [navigate])
  const onHover = useCallback((id: string | null) => setHovered(id), [])

  const showLabels = transform.k >= 0.7 || layout.nodes.length <= 36
  const neighbours = useMemo(() => {
    if (!hovered) return null
    const s = new Set<string>([hovered])
    for (const e of layout.edges) {
      if (e.source.id === hovered) s.add(e.target.id)
      if (e.target.id === hovered) s.add(e.source.id)
    }
    return s
  }, [hovered, layout.edges])

  const hoveredNode = hovered ? layout.nodes.find((n) => n.id === hovered) : null
  const hoveredEdge = hovered ? layout.edges.find((e) => e.target.id === hovered || e.source.id === hovered) : null

  const rings = [
    { r: ringRadius({ isSelf: false, online: true, path: 'direct' }, layout.baseRadius), label: 'direct' },
    { r: ringRadius({ isSelf: false, online: true, path: 'relay' }, layout.baseRadius), label: 'relayed' },
    { r: ringRadius({ isSelf: false, online: false, path: 'none' }, layout.baseRadius), label: 'offline' },
  ]

  const summary = `Topology map with ${layout.nodes.length} devices and ${layout.edges.length} links`

  return (
    <div ref={wrapRef} className={cn('relative h-full w-full overflow-hidden rounded-lg bg-surface-inset', className)}>
      {error && !nodes.length ? (
        <div className="absolute inset-0 flex items-center justify-center p-6">
          <ErrorState error={error} onRetry={onRetry} className="w-full max-w-md" />
        </div>
      ) : loading && !nodes.length ? (
        <div className="absolute inset-0 p-4" aria-busy="true" aria-label="Loading topology">
          <Skeleton className="size-full" rounded="lg" />
        </div>
      ) : !nodes.length ? (
        <div className="absolute inset-0 flex items-center justify-center p-6">
          <EmptyState
            bordered={false}
            icon={Waypoints}
            title={unfilteredCount ? 'No devices match these filters' : 'No devices in the topology yet'}
            description={unfilteredCount ? 'Relax the path or online filters to see more of the tailnet.' : 'The map fills in after the first collector poll.'}
          />
        </div>
      ) : (
        <>
          <svg
            ref={svgRef}
            width={size.width || undefined}
            height={size.height || undefined}
            viewBox={size.width && size.height ? `0 0 ${size.width} ${size.height}` : undefined}
            role="group"
            aria-label={summary}
            className={cn('block size-full touch-none select-none transition-opacity duration-300 cursor-grab active:cursor-grabbing', fetching && 'opacity-70')}
            onPointerDown={onPointerDown}
            onPointerMove={onPointerMove}
            onPointerUp={onPointerUp}
            onPointerCancel={onPointerUp}
            onClick={() => setHovered(null)}
          >
            <title>{summary}</title>
            <g transform={`translate(${transform.x} ${transform.y}) scale(${transform.k})`}>
              <g role="presentation" aria-hidden="true">
                {rings.map((ring) => (
                  <g key={ring.label}>
                    <circle r={ring.r} fill="none" stroke={theme.grid} strokeWidth={1} />
                    <text
                      x={ring.r * 0.7071 + 6}
                      y={ring.r * 0.7071 + 6}
                      dy={3}
                      textAnchor="start"
                      fontSize={9}
                      fontFamily={theme.font}
                      fill={theme.textMuted}
                      opacity={0.85}
                      stroke={theme.surface}
                      strokeWidth={3}
                      paintOrder="stroke"
                      style={{ textTransform: 'uppercase', letterSpacing: '0.08em' }}
                    >
                      {ring.label}
                    </text>
                  </g>
                ))}
              </g>
              <EdgeLayer edges={layout.edges} maxRate={maxRate} theme={theme} hovered={hovered} showLabels={showLabels} version={layout.version} />
              <g>
                {layout.nodes.map((n) => (
                  <NodeGlyph key={n.id} n={n} x={n.x} y={n.y} theme={theme} hovered={hovered === n.id} dimmed={!!neighbours && !neighbours.has(n.id)} showLabel={showLabels} onHover={onHover} onActivate={activate} />
                ))}
              </g>
            </g>
          </svg>

          {hoveredNode ? (
            <div
              role="tooltip"
              className="pointer-events-none absolute z-10 w-max max-w-[260px] rounded-lg border border-border bg-surface-raised px-3 py-2 text-xs shadow-lg animate-fade-in"
              style={(() => {
                const sx = hoveredNode.x * transform.k + transform.x
                const sy = hoveredNode.y * transform.k + transform.y
                const gap = (hoveredNode.r + 12) * transform.k + 8
                // Flip below the glyph when there is no room above it (tooltip ≈ 140px tall).
                const below = sy - gap < 150
                return {
                  left: Math.max(136, Math.min(size.width - 136, sx)),
                  top: below ? sy + gap + 8 : sy - gap,
                  transform: below ? 'translate(-50%, 0)' : 'translate(-50%, -100%)',
                }
              })()}
            >
              <p className="flex items-center gap-2 font-semibold text-fg">
                <span
                  aria-hidden="true"
                  className={cn('size-2 rounded-full', hoveredNode.node.online ? (hoveredNode.node.path === 'relay' ? 'bg-relay-fill' : 'bg-online-fill') : 'bg-offline-fill')}
                />
                <span className="truncate">{hoveredNode.node.name}</span>
                {hoveredNode.node.isSelf ? <span className="text-fg-muted">· hub</span> : null}
              </p>
              <dl className="mt-1 grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5 text-fg-secondary">
                <dt className="text-fg-muted">Status</dt>
                <dd>{hoveredNode.node.online ? edgePathLabel(hoveredNode.node.path, hoveredNode.node.relay) : 'Offline'}</dd>
                {typeof hoveredNode.node.latencyMs === 'number' ? (
                  <>
                    <dt className="text-fg-muted">Latency</dt>
                    <dd className="num text-fg">{formatLatency(hoveredNode.node.latencyMs)}</dd>
                  </>
                ) : null}
                {hoveredEdge ? (
                  <>
                    <dt className="text-fg-muted">Traffic</dt>
                    <dd className="num text-fg">
                      ↓ {formatBitrate(hoveredEdge.edge.rxRate)} · ↑ {formatBitrate(hoveredEdge.edge.txRate)}
                    </dd>
                  </>
                ) : null}
                <dt className="text-fg-muted">OS</dt>
                <dd>{osInfo(hoveredNode.node.os).label}</dd>
                <dt className="text-fg-muted">User</dt>
                <dd className="truncate">{hoveredNode.node.user}</dd>
              </dl>
            </div>
          ) : null}

          <div className="absolute right-2 top-2 flex flex-col gap-1 rounded-md border border-border bg-surface-raised p-0.5 shadow-sm">
            <IconButton icon={Plus} label="Zoom in" size="sm" tooltipSide="left" onClick={() => zoomBy(1.4)} />
            <IconButton icon={Minus} label="Zoom out" size="sm" tooltipSide="left" onClick={() => zoomBy(1 / 1.4)} />
            <IconButton icon={Maximize2} label="Fit to view" size="sm" tooltipSide="left" onClick={onFit} />
            <IconButton icon={RefreshCw} label="Re-run layout" size="sm" tooltipSide="left" onClick={onRelayout} />
          </div>

          <div className="pointer-events-none absolute bottom-2 left-2 flex flex-wrap items-center gap-x-3 gap-y-1 rounded-md border border-border bg-surface-raised/90 px-2.5 py-1.5 text-[11px] text-fg-secondary shadow-sm backdrop-blur-sm" aria-label="Legend">
            <span className="inline-flex items-center gap-1.5">
              <svg width="18" height="6" aria-hidden="true">
                <line x1="0" y1="3" x2="18" y2="3" stroke={theme.status.direct} strokeWidth="2" strokeLinecap="round" />
              </svg>
              Direct
            </span>
            <span className="inline-flex items-center gap-1.5">
              <svg width="18" height="6" aria-hidden="true">
                <line x1="0" y1="3" x2="18" y2="3" stroke={theme.status.relay} strokeWidth="2" strokeDasharray="4 3" strokeLinecap="round" />
              </svg>
              Relay (DERP)
            </span>
            <span className="inline-flex items-center gap-1.5">
              <svg width="14" height="14" aria-hidden="true">
                <circle cx="7" cy="7" r="5" fill="none" stroke={theme.status.offline} strokeWidth="1.5" strokeDasharray="3 3" />
              </svg>
              Offline
            </span>
            <span className="inline-flex items-center gap-1.5">
              <svg width="14" height="14" aria-hidden="true">
                <circle cx="7" cy="7" r="6" fill={theme.accent} />
              </svg>
              Hub
            </span>
            <span className="hidden text-fg-muted sm:inline">line width = throughput</span>
          </div>
        </>
      )}
    </div>
  )
}
