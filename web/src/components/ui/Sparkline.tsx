import { useEffect, useId, useMemo, useRef, useState } from 'react'
import { cn } from '../../lib/cn'

export interface SparklineProps {
  data: ReadonlyArray<number | null | undefined>
  /** Fixed pixel width; omit to fill the container. */
  width?: number
  height?: number
  /** Any CSS colour (hex, or `var(--online-fill)`); defaults to the accent. */
  color?: string
  /** Soft 10% area wash under the line (default true). */
  fill?: boolean
  strokeWidth?: number
  min?: number
  max?: number
  /** Mark the last non-null point with a ringed dot (default true). */
  showLast?: boolean
  className?: string
  /** Accessible description; the sparkline is otherwise aria-hidden. */
  ariaLabel?: string
}

/** Observe an element's content width. */
export function useElementWidth<T extends HTMLElement>(): [React.RefObject<T | null>, number] {
  const ref = useRef<T | null>(null)
  const [w, setW] = useState(0)
  useEffect(() => {
    const el = ref.current
    if (!el) return
    setW(el.getBoundingClientRect().width)
    if (typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver((entries) => {
      const e = entries[0]
      if (e) setW(e.contentRect.width)
    })
    ro.observe(el)
    return () => ro.disconnect()
  }, [])
  return [ref, w]
}

interface Geometry {
  line: string
  area: string
  last: { x: number; y: number } | null
}

/** Pure path builder (exported for tests). Null values create gaps. */
export function sparklinePaths(
  data: ReadonlyArray<number | null | undefined>,
  width: number,
  height: number,
  opts: { min?: number; max?: number; pad?: number } = {},
): Geometry {
  const pad = opts.pad ?? 2
  const vals = data.filter((v): v is number => typeof v === 'number' && Number.isFinite(v))
  if (!vals.length || width <= 0) return { line: '', area: '', last: null }
  const lo = opts.min ?? Math.min(...vals)
  const hi = opts.max ?? Math.max(...vals)
  const span = hi - lo || 1
  const n = data.length
  const stepX = n > 1 ? (width - pad * 2) / (n - 1) : 0
  const y = (v: number) => pad + (1 - (v - lo) / span) * (height - pad * 2)
  const x = (i: number) => pad + i * stepX
  const baseline = height - pad
  let line = ''
  let area = ''
  let segStart: number | null = null
  let last: Geometry['last'] = null
  const r2 = (v: number) => Math.round(v * 100) / 100
  data.forEach((v, i) => {
    const ok = typeof v === 'number' && Number.isFinite(v)
    if (ok) {
      const px = r2(x(i))
      const py = r2(y(v))
      if (segStart === null) {
        segStart = i
        line += `M${px} ${py}`
        area += `M${px} ${baseline}L${px} ${py}`
      } else {
        line += `L${px} ${py}`
        area += `L${px} ${py}`
      }
      last = { x: px, y: py }
    }
    const end = !ok || i === n - 1
    if (end && segStart !== null) {
      const lastIdx = ok ? i : i - 1
      area += `L${r2(x(lastIdx))} ${baseline}Z`
      segStart = null
    }
  })
  return { line, area, last }
}

/** Dependency-free SVG sparkline with gap support and an optional end marker. */
export function Sparkline({ data, width, height = 32, color = 'var(--accent)', fill = true, strokeWidth = 1.5, min, max, showLast = true, className, ariaLabel }: SparklineProps) {
  const [ref, measured] = useElementWidth<HTMLDivElement>()
  const w = width ?? measured
  const id = useId()
  const geo = useMemo(() => sparklinePaths(data, w, height, { min, max }), [data, w, height, min, max])
  return (
    <div ref={ref} className={cn('block', width ? '' : 'w-full', className)} style={{ width: width ?? undefined, height }}>
      {w > 0 ? (
        <svg width={w} height={height} viewBox={`0 0 ${w} ${height}`} role={ariaLabel ? 'img' : undefined} aria-label={ariaLabel} aria-hidden={ariaLabel ? undefined : true} className="block overflow-visible">
          {ariaLabel ? <title id={`${id}-t`}>{ariaLabel}</title> : null}
          {fill && geo.area ? <path d={geo.area} style={{ fill: color, fillOpacity: 0.1 }} /> : null}
          {geo.line ? <path d={geo.line} fill="none" style={{ stroke: color }} strokeWidth={strokeWidth} strokeLinejoin="round" strokeLinecap="round" /> : null}
          {showLast && geo.last ? <circle cx={geo.last.x} cy={geo.last.y} r={2.5} style={{ fill: color, stroke: 'var(--surface)' }} strokeWidth={2} /> : null}
        </svg>
      ) : null}
    </div>
  )
}
