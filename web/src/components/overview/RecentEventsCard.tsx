import { Link } from 'react-router-dom'
import { Activity, ArrowRight } from 'lucide-react'
import type { Event } from '../../api/types'
import { useEvents } from '../../api/hooks'
import { cn } from '../../lib/cn'
import { formatDateTime, formatRelative } from '../../lib/format'
import { eventTone, eventTypeLabel } from '../../lib/status'
import { TONE_SOFT_BG, TONE_TEXT } from '../ui/Badge'
import { Card, CardHeader } from '../ui/Card'
import { EmptyState } from '../ui/EmptyState'
import { ErrorState } from '../ui/ErrorState'
import { Skeleton } from '../ui/Skeleton'
import { eventIcon } from './overview'

function EventRow({ event: e }: { event: Event }) {
  const tone = eventTone(e.type, e.severity)
  const Icon = eventIcon(e.type)
  return (
    <li className="flex items-start gap-3 px-4 py-2.5 sm:px-5">
      <span className={cn('mt-0.5 flex size-6 shrink-0 items-center justify-center rounded-md', TONE_SOFT_BG[tone], TONE_TEXT[tone])} title={eventTypeLabel(e.type)}>
        <Icon className="size-3.5" aria-hidden="true" />
        <span className="sr-only">{eventTypeLabel(e.type)}</span>
      </span>
      <div className="min-w-0 flex-1">
        <p className="truncate text-[13px] leading-5 text-fg" title={e.title}>
          {e.title}
        </p>
        <p className="flex items-center gap-1.5 text-xs text-fg-muted">
          {e.deviceId && e.deviceName ? (
            <>
              <Link to={`/devices/${encodeURIComponent(e.deviceId)}`} className="max-w-[160px] truncate rounded font-medium text-fg-secondary hover:text-accent-text focus-ring">
                {e.deviceName}
              </Link>
              <span aria-hidden="true">·</span>
            </>
          ) : null}
          <time dateTime={e.ts} title={formatDateTime(e.ts)} className="num whitespace-nowrap">
            {formatRelative(e.ts)}
          </time>
        </p>
      </div>
    </li>
  )
}

/** Newest events, fed live by the SSE stream. */
export function RecentEventsCard({ limit = 8 }: { limit?: number }) {
  const q = useEvents({ limit: 25 })
  const rows = (q.data ?? []).slice(0, limit)
  return (
    <Card padding="none">
      <CardHeader
        divider
        title="Recent events"
        description="Live activity across the tailnet"
        actions={
          <Link to="/events" className="inline-flex items-center gap-1 rounded px-1 text-xs font-medium text-accent-text hover:underline focus-ring">
            All events
            <ArrowRight className="size-3" aria-hidden="true" />
          </Link>
        }
      />
      {q.error && !q.data ? (
        <div className="p-4">
          <ErrorState compact error={q.error} onRetry={() => void q.refetch()} retrying={q.isFetching} />
        </div>
      ) : q.isPending ? (
        <ul className="divide-y divide-border-subtle" aria-busy="true" aria-label="Loading events">
          {Array.from({ length: 5 }, (_, i) => (
            <li key={i} className="flex items-center gap-3 px-4 py-2.5 sm:px-5" aria-hidden="true">
              <Skeleton width={24} height={24} rounded="md" />
              <div className="flex-1 space-y-1.5">
                <Skeleton height={12} width="75%" />
                <Skeleton height={10} width="40%" />
              </div>
            </li>
          ))}
        </ul>
      ) : rows.length === 0 ? (
        <EmptyState size="sm" bordered={false} icon={Activity} title="No events yet" description="Device, alert and admin activity will show up here." />
      ) : (
        <ul className="divide-y divide-border-subtle" aria-label="Recent events">
          {rows.map((e) => (
            <EventRow key={e.id} event={e} />
          ))}
        </ul>
      )}
    </Card>
  )
}
