import { useId, useMemo, useState, type ReactNode } from 'react'
import { cn } from '../../lib/cn'
import { formatInt, formatPercent } from '../../lib/format'
import type { StatusTone } from '../../lib/status'
import { seriesColor, toneColor, useChartTheme } from './ChartTheme'

export interface DonutSlice {
  label: string
  value: number
  color?: string
  tone?: StatusTone
}

export interface DonutChartProps {
  data: ReadonlyArray<DonutSlice>
  size?: number
  thickness?: number
  centerLabel?: ReactNode
  centerValue?: ReactNode
  /** Legend with values and shares (default true). */
  legend?: boolean
  format?: (v: number) => string
  /** Fold slices beyond this count into "Other" (default 6). */
  maxSlices?: number
  ariaLabel?: string
  className?: string
}

/**
 * Part-to-whole donut (≤6 slices, 2px surface gaps between segments). Hovering
 * a slice or legend row shows that slice in the centre; a legend always carries
 * the values so colour is never the only channel.
 */
export function DonutChart({ data, size = 140, thickness = 14, centerLabel, centerValue, legend = true, format = formatInt, maxSlices = 6, ariaLabel, className }: DonutChartProps) {
  const theme = useChartTheme()
  const id = useId()
  const [hover, setHover] = useState<number | null>(null)

  const slices = useMemo(() => {
    const sorted = [...data].filter((d) => d.value > 0).sort((a, b) => b.value - a.value)
    const head = sorted.slice(0, maxSlices - (sorted.length > maxSlices ? 1 : 0))
    const tail = sorted.slice(head.length)
    const out = head.map((s, i) => ({ ...s, color: s.color ?? (s.tone ? toneColor(theme, s.tone) : seriesColor(theme, i)) }))
    if (tail.length) out.push({ label: 'Other', value: tail.reduce((a, b) => a + b.value, 0), color: theme.muted })
    return out
  }, [data, maxSlices, theme])

  const total = slices.reduce((a, b) => a + b.value, 0)
  const r = size / 2 - thickness / 2
  const c = 2 * Math.PI * r
  const gap = slices.length > 1 ? 2 : 0

  let offset = 0
  const arcs = slices.map((s) => {
    const len = total ? (s.value / total) * c : 0
    const arc = { ...s, dash: Math.max(0, len - gap), off: -offset, len }
    offset += len
    return arc
  })

  const active = hover !== null ? slices[hover] : null
  const empty = total === 0

  return (
    <div className={cn('flex flex-wrap items-center gap-5', className)}>
      <div className="relative shrink-0" style={{ width: size, height: size }}>
        <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} role="img" aria-labelledby={`${id}-title`} className="block -rotate-90">
          <title id={`${id}-title`}>{ariaLabel ?? `${slices.length} segments, total ${format(total)}`}</title>
          <circle cx={size / 2} cy={size / 2} r={r} fill="none" stroke={theme.grid} strokeWidth={thickness} />
          {arcs.map((a, i) => (
            <circle
              key={i}
              cx={size / 2}
              cy={size / 2}
              r={r}
              fill="none"
              stroke={a.color}
              strokeWidth={thickness}
              strokeDasharray={`${a.dash} ${c - a.dash}`}
              strokeDashoffset={a.off}
              opacity={hover === null || hover === i ? 1 : 0.35}
              className="transition-opacity"
              onPointerEnter={() => setHover(i)}
              onPointerLeave={() => setHover(null)}
            >
              <title>{`${a.label}: ${format(a.value)} (${formatPercent((a.value / total) * 100)})`}</title>
            </circle>
          ))}
        </svg>
        <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center text-center" aria-hidden="true">
          {active ? (
            <>
              <span className="text-lg font-semibold leading-6 text-fg">{format(active.value)}</span>
              <span className="max-w-[70%] truncate text-[11px] text-fg-muted">{active.label}</span>
            </>
          ) : (
            <>
              <span className="text-lg font-semibold leading-6 text-fg">{centerValue ?? (empty ? '—' : format(total))}</span>
              {centerLabel ? <span className="text-[11px] text-fg-muted">{centerLabel}</span> : null}
            </>
          )}
        </div>
      </div>
      {legend ? (
        <ul className="min-w-0 flex-1 space-y-1 text-xs">
          {empty ? <li className="text-fg-muted">No data</li> : null}
          {slices.map((s, i) => (
            <li
              key={i}
              onPointerEnter={() => setHover(i)}
              onPointerLeave={() => setHover(null)}
              className={cn('flex items-center gap-2 rounded px-1 py-0.5 transition-colors', hover === i && 'bg-surface-hover')}
            >
              <span aria-hidden="true" className="size-2 shrink-0 rounded-[2px]" style={{ background: s.color }} />
              <span className="min-w-0 flex-1 truncate text-fg-secondary">{s.label}</span>
              <span className="num text-fg">{format(s.value)}</span>
              <span className="num w-9 text-right text-fg-muted">{formatPercent((s.value / total) * 100)}</span>
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  )
}
