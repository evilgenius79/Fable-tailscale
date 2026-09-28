import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'
import type { LucideIcon } from 'lucide-react'
import { cn } from '../../lib/cn'
import { formatInt } from '../../lib/format'
import type { StatusTone } from '../../lib/status'
import { EmptyState } from '../ui/EmptyState'
import { seriesColor, toneColor, useChartTheme } from './ChartTheme'

export interface BarListItem {
  key?: string
  label: ReactNode
  value: number
  /** Per-item colour override (use tone for status semantics). */
  color?: string
  tone?: StatusTone
  to?: string
  icon?: LucideIcon
  sublabel?: ReactNode
}

export interface BarListProps {
  items: ReadonlyArray<BarListItem>
  format?: (v: number) => string
  /** Scale bars against this value instead of the max item. */
  max?: number
  /** Sort descending by value (default true). */
  sort?: boolean
  limit?: number
  showValues?: boolean
  /** Default bar colour: categorical slot 1 (nominal categories share one hue). */
  color?: string
  emptyMessage?: ReactNode
  ariaLabel?: string
  className?: string
}

/**
 * Ranked horizontal bars (nominal categories → one hue). Thin 8px marks with a
 * rounded data-end and a square baseline; the label + value are always visible.
 */
export function BarList({ items, format = formatInt, max, sort = true, limit, showValues = true, color, emptyMessage = 'Nothing to show', ariaLabel, className }: BarListProps) {
  const theme = useChartTheme()
  const base = color ?? seriesColor(theme, 0)
  let rows = sort ? [...items].sort((a, b) => b.value - a.value) : [...items]
  if (limit) rows = rows.slice(0, limit)
  const top = max ?? Math.max(0, ...rows.map((r) => r.value))
  if (!rows.length) return <EmptyState size="sm" bordered={false} title={emptyMessage} className={className} />
  return (
    <ul className={cn('space-y-2', className)} aria-label={ariaLabel}>
      {rows.map((r, i) => {
        const pct = top > 0 ? (r.value / top) * 100 : 0
        const fill = r.color ?? (r.tone ? toneColor(theme, r.tone) : base)
        const Icon = r.icon
        const label = (
          <span className="flex min-w-0 items-center gap-1.5">
            {Icon ? <Icon className="size-3.5 shrink-0 text-fg-muted" aria-hidden="true" /> : null}
            <span className="truncate">{r.label}</span>
            {r.sublabel ? <span className="truncate text-fg-muted">{r.sublabel}</span> : null}
          </span>
        )
        return (
          <li key={r.key ?? i} className="group grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-3 gap-y-1 text-[13px]">
            <div className="min-w-0 text-fg">
              {r.to ? (
                <Link to={r.to} className="block truncate rounded hover:text-accent-text focus-ring">
                  {label}
                </Link>
              ) : (
                label
              )}
            </div>
            {showValues ? <span className="num text-xs text-fg-secondary">{format(r.value)}</span> : <span />}
            <div className="col-span-2 h-2 w-full overflow-hidden rounded-r-[4px] bg-surface-inset" role="presentation">
              <div className="h-full rounded-r-[4px] transition-[width] duration-300" style={{ width: `${pct}%`, background: fill, minWidth: r.value > 0 ? 2 : 0 }} />
            </div>
          </li>
        )
      })}
    </ul>
  )
}
