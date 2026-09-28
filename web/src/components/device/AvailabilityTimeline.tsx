import { useMemo } from 'react'
import { Activity } from 'lucide-react'
import type { UptimeReport } from '../../api/types'
import { cn } from '../../lib/cn'
import { formatDateTime, formatDuration, formatInt, formatRelative, formatUptimePct, plural } from '../../lib/format'
import { formatTick, rangePreset, type RangeKey } from '../../lib/time'
import { Card, CardHeader } from '../ui/Card'
import { EmptyState } from '../ui/EmptyState'
import { ErrorState } from '../ui/ErrorState'
import { Skeleton } from '../ui/Skeleton'
import { Tooltip } from '../ui/Tooltip'
import { outageList, timelineBars, timelineTicks } from './deviceDetail'

export interface AvailabilityTimelineProps {
  report: UptimeReport | undefined
  range: RangeKey
  loading?: boolean
  fetching?: boolean
  error?: unknown
  onRetry?: () => void
  className?: string
}

/**
 * Online/offline segments across the selected window. Segments are the marks
 * (status colours + a legend, never colour alone); outages are focusable and
 * also listed as text below so the tooltip never gates a value.
 */
export function AvailabilityTimeline({ report, range, loading, fetching, error, onRetry, className }: AvailabilityTimelineProps) {
  const bars = useMemo(() => timelineBars(report), [report])
  const outages = useMemo(() => outageList(report), [report])
  const ticks = useMemo(() => (report ? timelineTicks(report.from, report.to, 5) : []), [report])
  const preset = rangePreset(range)

  const summary = report ? (
    <div className="flex items-baseline gap-3">
      <span className="text-2xl font-semibold tracking-tight text-fg">{formatUptimePct(report.pct)}</span>
      <span className="text-xs text-fg-muted num">{report.outages === 0 ? 'no outages' : plural(report.outages, 'outage')}</span>
    </div>
  ) : loading ? (
    <Skeleton height={28} width={96} />
  ) : null

  return (
    <Card className={cn('min-w-0', className)}>
      <CardHeader title="Availability" description={`Online time over the ${preset.longLabel.toLowerCase()}`} icon={Activity} actions={summary} />
      {error && !report ? (
        <ErrorState compact error={error} onRetry={onRetry} />
      ) : loading && !report ? (
        <div aria-busy="true" aria-label="Loading availability">
          <Skeleton height={24} rounded="md" />
          <div className="mt-2 flex justify-between">
            {Array.from({ length: 5 }, (_, i) => (
              <Skeleton key={i} height={8} width={40} />
            ))}
          </div>
        </div>
      ) : !bars.length ? (
        <EmptyState size="sm" bordered={false} icon={Activity} title="No availability data for this range" />
      ) : (
        <div className={cn('transition-opacity duration-300', fetching && 'opacity-60')}>
          <div
            role="img"
            aria-label={`Availability timeline: ${formatUptimePct(report!.pct)} online, ${plural(report!.outages, 'outage')}`}
            className="relative h-6 w-full overflow-hidden rounded-md bg-surface-inset"
          >
            {bars.map((b, i) => {
              const label = `${b.online ? 'Online' : 'Offline'} · ${formatDateTime(b.from)} → ${formatDateTime(b.to)} · ${formatDuration(b.seconds)}`
              const style = { left: `${b.startPct}%`, width: `max(${b.widthPct}%, 2px)` }
              const cls = cn('absolute inset-y-0 transition-opacity hover:opacity-80', b.online ? 'bg-online-fill' : 'bg-critical-fill')
              return b.online ? (
                <Tooltip key={i} content={label}>
                  <div className={cls} style={style} aria-hidden="true" />
                </Tooltip>
              ) : (
                <Tooltip key={i} content={label}>
                  <button type="button" className={cn(cls, 'focus-ring-inset')} style={style} aria-label={label} />
                </Tooltip>
              )
            })}
            {/* 2px surface gaps between segments */}
            {bars.slice(1).map((b, i) => (
              <span key={`gap-${i}`} aria-hidden="true" className="pointer-events-none absolute inset-y-0 w-0.5 -translate-x-1/2 bg-surface" style={{ left: `${b.startPct}%` }} />
            ))}
          </div>
          {ticks.length ? (
            <div className="mt-1.5 flex justify-between text-[11px] text-fg-muted num" aria-hidden="true">
              {ticks.map((t, i) => (
                <span key={i} className={cn(i === 0 ? '' : i === ticks.length - 1 ? 'text-right' : 'hidden text-center sm:inline')}>
                  {formatTick(Math.round(t / 1000), range)}
                </span>
              ))}
            </div>
          ) : null}
          <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-1.5 text-xs text-fg-secondary">
            <span className="inline-flex items-center gap-1.5">
              <span aria-hidden="true" className="size-2 rounded-[2px] bg-online-fill" />
              Online
            </span>
            <span className="inline-flex items-center gap-1.5">
              <span aria-hidden="true" className="size-2 rounded-[2px] bg-critical-fill" />
              Offline
            </span>
            {report?.from ? (
              <span className="ml-auto text-fg-muted num">
                {formatDateTime(report.from)} – {formatDateTime(report.to)}
              </span>
            ) : null}
          </div>
          {outages.length ? (
            <ol className="mt-3 divide-y divide-border-subtle border-t border-border-subtle text-[13px]" aria-label="Outages">
              {outages.slice(0, 4).map((o, i) => (
                <li key={i} className="flex flex-wrap items-center justify-between gap-x-4 gap-y-0.5 py-1.5">
                  <span className="text-fg num">
                    {formatDateTime(o.from)} <span className="text-fg-faint">→</span> {o.ongoing ? <span className="text-critical">still offline</span> : formatDateTime(o.to)}
                  </span>
                  <span className="text-fg-muted num">
                    {formatDuration(o.seconds)} · {formatRelative(o.from)}
                  </span>
                </li>
              ))}
              {outages.length > 4 ? <li className="py-1.5 text-xs text-fg-muted">and {formatInt(outages.length - 4)} more</li> : null}
            </ol>
          ) : null}
        </div>
      )}
    </Card>
  )
}
