import { cn } from '../../lib/cn'
import { formatInt, formatPercent } from '../../lib/format'
import { seriesColor, useChartTheme } from '../charts/ChartTheme'
import { Tooltip } from '../ui/Tooltip'

export interface StackedBarSegment {
  key: string
  label: string
  value: number
}

export interface StackedBarProps {
  segments: ReadonlyArray<StackedBarSegment>
  format?: (v: number) => string
  ariaLabel: string
  className?: string
}

/**
 * Part-to-whole stacked bar (dataviz): categorical slots in fixed order, a 2px
 * surface gap between segments, 4px rounded ends, and a legend that always
 * carries label + value + share so colour is never the only channel.
 */
export function StackedBar({ segments, format = formatInt, ariaLabel, className }: StackedBarProps) {
  const theme = useChartTheme()
  const total = segments.reduce((a, s) => a + Math.max(0, s.value), 0)
  const rows = segments.map((s, i) => ({ ...s, color: seriesColor(theme, i), share: total > 0 ? (Math.max(0, s.value) / total) * 100 : 0 }))
  const summary = rows.map((r) => `${r.label} ${format(r.value)} (${formatPercent(r.share)})`).join(', ')
  return (
    <div className={cn('space-y-3', className)}>
      <div role="img" aria-label={`${ariaLabel}: ${summary}`} className="flex h-2.5 w-full gap-0.5">
        {total > 0 ? (
          rows
            .filter((r) => r.value > 0)
            .map((r, i, arr) => (
              <Tooltip
                key={r.key}
                content={
                  <span>
                    {r.label}: <span className="num">{format(r.value)}</span> · {formatPercent(r.share, 1)}
                  </span>
                }
              >
                <div
                  className={cn('h-full min-w-[2px] transition-[width] duration-300', i === 0 && 'rounded-l-[4px]', i === arr.length - 1 && 'rounded-r-[4px]')}
                  style={{ width: `${r.share}%`, background: r.color }}
                  tabIndex={0}
                  aria-hidden="true"
                />
              </Tooltip>
            ))
        ) : (
          <div className="h-full w-full rounded-[4px] bg-surface-inset" />
        )}
      </div>
      <ul className="grid grid-cols-1 gap-x-6 gap-y-1.5 text-xs sm:grid-cols-2" aria-hidden="true">
        {rows.map((r) => (
          <li key={r.key} className="flex min-w-0 items-center gap-2">
            <span className="size-2.5 shrink-0 rounded-[3px]" style={{ background: r.color }} />
            <span className="truncate text-fg-secondary">{r.label}</span>
            <span className="num ml-auto shrink-0 text-fg">{format(r.value)}</span>
            <span className="num w-10 shrink-0 text-right text-fg-muted">{formatPercent(r.share)}</span>
          </li>
        ))}
      </ul>
    </div>
  )
}
