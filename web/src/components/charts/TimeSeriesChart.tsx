import { useMemo, useState, type ReactNode } from 'react'
import { Area, CartesianGrid, ComposedChart, Line, ReferenceLine, ResponsiveContainer, Tooltip, XAxis, YAxis, type TooltipContentProps } from 'recharts'
import { LineChart as LineChartIcon, TableProperties } from 'lucide-react'
import { cn } from '../../lib/cn'
import { formatDateTime, formatNumber } from '../../lib/format'
import { formatTick, rangeSeconds, type RangeKey } from '../../lib/time'
import type { StatusTone } from '../../lib/status'
import { EmptyState } from '../ui/EmptyState'
import { ErrorState } from '../ui/ErrorState'
import { Skeleton } from '../ui/Skeleton'
import { CHART_MARGIN, MARK, seriesColor, toneColor, useChartTheme } from './ChartTheme'

export interface TimeSeriesDatum {
  /** Unix seconds. */
  t: number
  [key: string]: number | null | undefined
}

export interface TimeSeriesSeries {
  key: string
  label: string
  /** Explicit colour; otherwise the categorical slot at this series' index. */
  color?: string
  /** Use a status tone instead of a categorical slot (e.g. latency = info). */
  tone?: StatusTone
  kind?: 'line' | 'area'
  dashed?: boolean
  /** Value formatter for tooltip + table (defaults to the chart's yFormat). */
  format?: (v: number) => string
}

export interface TimeSeriesChartProps {
  data: ReadonlyArray<TimeSeriesDatum>
  series: ReadonlyArray<TimeSeriesSeries>
  /** Picks tick formatting and tick spacing. */
  range: RangeKey
  /** Total height including the x-axis band (default 220). */
  height?: number
  yFormat?: (v: number) => string
  /** Default [0, 'auto']. */
  yDomain?: [number | 'auto', number | 'auto']
  /** Stack areas (part-to-whole). */
  stacked?: boolean
  /** Default: shown when there is more than one series. */
  showLegend?: boolean
  referenceLines?: ReadonlyArray<{ y: number; label?: string; tone?: StatusTone }>
  loading?: boolean
  /** Hold the previous render at reduced opacity while refetching (no skeleton flash). */
  fetching?: boolean
  error?: unknown
  onRetry?: () => void
  emptyMessage?: ReactNode
  /** Share crosshair across charts. */
  syncId?: string
  /** Offer the table-view twin (default true). */
  tableView?: boolean
  /** Fixed y-axis width in px; estimated from the formatted domain when omitted. */
  yAxisWidth?: number
  ariaLabel?: string
  className?: string
}

const STEPS = [60, 120, 300, 600, 900, 1800, 3600, 7200, 10800, 21600, 43200, 86400, 172800, 604800]

/** Nice, boundary-aligned unix-second ticks for [min, max] (exported for tests). */
export function timeTicks(min: number, max: number, count = 6): number[] {
  if (!(max > min) || count < 1) return []
  const span = max - min
  const step = STEPS.find((s) => span / s <= count) ?? STEPS[STEPS.length - 1]!
  const tzOffset = new Date(min * 1000).getTimezoneOffset() * 60
  const first = Math.ceil((min - tzOffset) / step) * step + tzOffset
  const out: number[] = []
  for (let t = first; t <= max; t += step) out.push(t)
  return out
}

function ChartTooltip({
  active,
  payload,
  label,
  series,
  yFormat,
  theme,
}: TooltipContentProps<number, string> & { series: ReadonlyArray<TimeSeriesSeries & { color: string }>; yFormat: (v: number) => string; theme: ReturnType<typeof useChartTheme> }) {
  if (!active || !payload?.length) return null
  const t = typeof label === 'number' ? label : Number(label)
  return (
    <div className="min-w-[160px] rounded-lg border border-border bg-surface-raised px-3 py-2 shadow-lg" style={{ fontFamily: theme.font }}>
      <p className="mb-1.5 text-[11px] font-medium text-fg-muted">{Number.isFinite(t) ? formatDateTime(t) : String(label)}</p>
      <ul className="space-y-1">
        {series.map((s) => {
          const entry = payload.find((p) => p.dataKey === s.key)
          const v = entry?.value
          const missing = v === null || v === undefined || typeof v !== 'number' || !Number.isFinite(v)
          return (
            <li key={s.key} className="flex items-center justify-between gap-4 text-xs">
              <span className="flex items-center gap-2 text-fg-secondary">
                <span aria-hidden="true" className="inline-block h-0.5 w-3 rounded-full" style={{ background: s.color }} />
                {s.label}
              </span>
              <span className={cn('num font-semibold', missing ? 'text-fg-faint' : 'text-fg')}>{missing ? '—' : (s.format ?? yFormat)(v)}</span>
            </li>
          )
        })}
      </ul>
    </div>
  )
}

/**
 * Line/area chart over unix-second `t`. One y-axis only; nulls render as gaps;
 * crosshair tooltip lists every series; legend for ≥2 series with toggle-to-hide;
 * table-view twin for accessibility.
 */
export function TimeSeriesChart({
  data,
  series,
  range,
  height = 220,
  yFormat = (v) => formatNumber(v, 1),
  yDomain = [0, 'auto'],
  stacked,
  showLegend,
  referenceLines,
  loading,
  fetching,
  error,
  onRetry,
  emptyMessage = 'No data for this range',
  syncId,
  tableView = true,
  yAxisWidth,
  ariaLabel,
  className,
}: TimeSeriesChartProps) {
  const theme = useChartTheme()
  const [hidden, setHidden] = useState<Set<string>>(() => new Set())
  const [showTable, setShowTable] = useState(false)

  const colored = useMemo(
    () => series.map((s, i) => ({ ...s, color: s.color ?? (s.tone ? toneColor(theme, s.tone) : seriesColor(theme, i)) })),
    [series, theme],
  )

  // Normalise undefined → null so Recharts breaks the line consistently.
  const rows = useMemo(() => {
    const keys = series.map((s) => s.key)
    return data.map((d) => {
      const o: TimeSeriesDatum = { t: d.t }
      for (const k of keys) {
        const v = d[k]
        o[k] = typeof v === 'number' && Number.isFinite(v) ? v : null
      }
      return o
    })
  }, [data, series])

  const hasAny = useMemo(() => rows.some((r) => series.some((s) => r[s.key] !== null)), [rows, series])
  const [tMin, tMax] = useMemo(() => {
    if (!rows.length) return [0, 0]
    const first = rows[0]!.t
    const last = rows[rows.length - 1]!.t
    // Pad the domain to the full range when the data is shorter, so gaps at the start show.
    return [Math.min(first, last - rangeSeconds(range)), last]
  }, [rows, range])
  const ticks = useMemo(() => timeTicks(tMin, tMax, 6), [tMin, tMax])

  // Estimate the y-axis width from the widest plausible tick label so labels
  // never clip after a container resize (Recharts' auto width re-measures lazily).
  const yWidth = useMemo(() => {
    if (yAxisWidth) return yAxisWidth
    let max = typeof yDomain[1] === 'number' ? yDomain[1] : 0
    if (typeof yDomain[1] !== 'number') {
      for (const r of rows) for (const s of series) {
        const v = r[s.key]
        if (typeof v === 'number' && v > max) max = v
      }
      if (stacked) max *= series.length
    }
    const samples = [0, max, max * 0.75, max * 0.5, max * 0.25, max * 1.2]
    const longest = Math.max(1, ...samples.map((v) => yFormat(v).length))
    return Math.max(28, Math.ceil(longest * 6.6) + 10)
  }, [yAxisWidth, yDomain, rows, series, stacked, yFormat])

  const legend = showLegend ?? series.length > 1
  const visible = colored.filter((s) => !hidden.has(s.key))

  const box = 'w-full'
  const frame = (child: ReactNode) => (
    <div className={cn('relative', box, className)}>
      {child}
    </div>
  )

  if (error) return frame(<ErrorState compact error={error} onRetry={onRetry} className="my-2" />)
  if (loading && !rows.length) {
    return frame(
      <div style={{ height }} className="flex flex-col justify-end gap-2 p-2" aria-busy="true" aria-label="Loading chart">
        <Skeleton className="w-full flex-1" />
        <div className="flex justify-between">
          {Array.from({ length: 5 }, (_, i) => (
            <Skeleton key={i} height={8} width={36} />
          ))}
        </div>
      </div>,
    )
  }
  if (!hasAny) {
    return frame(
      <div style={{ height }} className="flex items-center justify-center">
        <EmptyState size="sm" bordered={false} icon={LineChartIcon} title={emptyMessage} />
      </div>,
    )
  }

  const table = (
    <div className="overflow-auto scrollbar-thin rounded-md border border-border" style={{ maxHeight: height }}>
      <table className="w-full border-collapse text-xs">
        <thead className="sticky top-0 bg-surface">
          <tr>
            <th scope="col" className="hairline-b px-2 py-1.5 text-left font-semibold text-fg-muted">
              Time
            </th>
            {visible.map((s) => (
              <th key={s.key} scope="col" className="hairline-b px-2 py-1.5 text-right font-semibold text-fg-muted">
                {s.label}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {[...rows].reverse().map((r) => (
            <tr key={r.t} className="border-b border-border-subtle last:border-b-0">
              <td className="whitespace-nowrap px-2 py-1 text-fg-secondary">{formatDateTime(r.t)}</td>
              {visible.map((s) => {
                const v = r[s.key]
                return (
                  <td key={s.key} className="num px-2 py-1 text-right text-fg">
                    {typeof v === 'number' ? (s.format ?? yFormat)(v) : '—'}
                  </td>
                )
              })}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )

  return frame(
    <>
      <div className={cn('transition-opacity duration-300', fetching && 'opacity-60')} style={{ height }} role="img" aria-label={ariaLabel}>
        {showTable ? (
          table
        ) : (
          <ResponsiveContainer width="100%" height="100%">
            <ComposedChart data={rows} margin={CHART_MARGIN} syncId={syncId} accessibilityLayer>
              <CartesianGrid stroke={theme.grid} strokeWidth={MARK.gridWidth} vertical={false} />
              <XAxis
                dataKey="t"
                type="number"
                domain={[tMin, tMax]}
                ticks={ticks}
                tickFormatter={(v: number) => formatTick(v, range)}
                axisLine={{ stroke: theme.axis }}
                tickLine={false}
                tick={{ fill: theme.textMuted, fontSize: theme.fontSize, fontFamily: theme.font }}
                minTickGap={28}
                height={22}
              />
              <YAxis
                width={yWidth}
                domain={yDomain}
                tickFormatter={(v: number) => yFormat(v)}
                axisLine={false}
                tickLine={false}
                tick={{ fill: theme.textMuted, fontSize: theme.fontSize, fontFamily: theme.font }}
                tickCount={5}
                allowDecimals
              />
              <Tooltip
                cursor={{ stroke: theme.axis, strokeWidth: 1 }}
                isAnimationActive={false}
                content={(p) => <ChartTooltip {...(p as TooltipContentProps<number, string>)} series={visible} yFormat={yFormat} theme={theme} />}
                wrapperStyle={{ outline: 'none', zIndex: 20 }}
              />
              {referenceLines?.map((r, i) => (
                <ReferenceLine
                  key={i}
                  y={r.y}
                  stroke={r.tone ? toneColor(theme, r.tone) : theme.textMuted}
                  strokeWidth={1}
                  strokeDasharray="3 3"
                  ifOverflow="extendDomain"
                  label={r.label ? { value: r.label, position: 'insideTopRight', fill: theme.textMuted, fontSize: 10, fontFamily: theme.font } : undefined}
                />
              ))}
              {visible.map((s) =>
                s.kind === 'area' ? (
                  <Area
                    key={s.key}
                    type="monotone"
                    dataKey={s.key}
                    name={s.label}
                    stroke={s.color}
                    strokeWidth={MARK.lineWidth}
                    fill={s.color}
                    fillOpacity={MARK.areaOpacity}
                    stackId={stacked ? 'stack' : undefined}
                    dot={false}
                    activeDot={{ r: MARK.markerRadius, strokeWidth: 2, stroke: theme.surface, fill: s.color }}
                    connectNulls={false}
                    isAnimationActive={false}
                    strokeDasharray={s.dashed ? '4 3' : undefined}
                  />
                ) : (
                  <Line
                    key={s.key}
                    type="monotone"
                    dataKey={s.key}
                    name={s.label}
                    stroke={s.color}
                    strokeWidth={MARK.lineWidth}
                    dot={false}
                    activeDot={{ r: MARK.markerRadius, strokeWidth: 2, stroke: theme.surface, fill: s.color }}
                    connectNulls={false}
                    isAnimationActive={false}
                    strokeDasharray={s.dashed ? '4 3' : undefined}
                  />
                ),
              )}
            </ComposedChart>
          </ResponsiveContainer>
        )}
      </div>
      {legend || tableView ? (
        <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1.5 text-xs">
          {legend
            ? colored.map((s) => {
                const off = hidden.has(s.key)
                return (
                  <button
                    key={s.key}
                    type="button"
                    aria-pressed={!off}
                    onClick={() =>
                      setHidden((prev) => {
                        const next = new Set(prev)
                        if (next.has(s.key)) next.delete(s.key)
                        else if (next.size < colored.length - 1) next.add(s.key)
                        return next
                      })
                    }
                    className={cn('inline-flex items-center gap-1.5 rounded px-1 py-0.5 text-fg-secondary hover:text-fg focus-ring', off && 'line-through opacity-50')}
                  >
                    <span aria-hidden="true" className="inline-block h-0.5 w-3.5 rounded-full" style={{ background: s.color }} />
                    {s.label}
                  </button>
                )
              })
            : null}
          {tableView ? (
            <button
              type="button"
              aria-pressed={showTable}
              onClick={() => setShowTable((v) => !v)}
              className="ml-auto inline-flex items-center gap-1 rounded px-1 py-0.5 text-fg-muted hover:text-fg focus-ring"
            >
              {showTable ? <LineChartIcon className="size-3.5" aria-hidden="true" /> : <TableProperties className="size-3.5" aria-hidden="true" />}
              {showTable ? 'Chart' : 'Table'}
            </button>
          ) : null}
        </div>
      ) : null}
    </>,
  )
}
