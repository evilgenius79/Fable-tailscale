import { useEffect, useRef } from 'react'
import { Activity, ArrowUp, ChevronDown, SearchX } from 'lucide-react'
import { cn } from '../../lib/cn'
import { formatInt, plural } from '../../lib/format'
import { Button } from '../ui/Button'
import { EmptyState } from '../ui/EmptyState'
import { ErrorState } from '../ui/ErrorState'
import { Skeleton } from '../ui/Skeleton'
import { EventRow } from './EventRow'
import type { EventDayGroup } from './events'

export interface EventTimelineProps {
  groups: EventDayGroup[]
  loading: boolean
  fetching?: boolean
  error: unknown
  onRetry: () => void
  hasMore: boolean
  loadingMore: boolean
  loadMoreError: unknown
  onLoadMore: () => void
  filtering: boolean
  onClearFilters: () => void
  newIds: ReadonlySet<number>
  now: number
}

function SkeletonRows({ n = 6 }: { n?: number }) {
  return (
    <ul className="surface-card divide-y divide-border-subtle" aria-busy="true" aria-label="Loading events">
      {Array.from({ length: n }, (_, i) => (
        <li key={i} className="flex items-start gap-3 px-4 py-3 sm:px-5" aria-hidden="true">
          <Skeleton width={32} height={32} rounded="md" />
          <div className="flex-1 space-y-2 pt-1">
            <Skeleton height={12} width={`${55 + ((i * 13) % 30)}%`} />
            <Skeleton height={10} width={`${30 + ((i * 7) % 25)}%`} />
          </div>
          <Skeleton height={10} width={44} />
        </li>
      ))}
    </ul>
  )
}

/** Day-separated timeline with an infinite-scroll sentinel and a "load more" fallback. */
export function EventTimeline({ groups, loading, fetching, error, onRetry, hasMore, loadingMore, loadMoreError, onLoadMore, filtering, onClearFilters, newIds, now }: EventTimelineProps) {
  const sentinel = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const el = sentinel.current
    if (!el || !hasMore || loadingMore || typeof IntersectionObserver === 'undefined') return
    const io = new IntersectionObserver(
      (entries) => {
        if (entries.some((e) => e.isIntersecting)) onLoadMore()
      },
      { rootMargin: '400px 0px' },
    )
    io.observe(el)
    return () => io.disconnect()
  }, [hasMore, loadingMore, onLoadMore, groups.length])

  if (error) return <ErrorState error={error} onRetry={onRetry} />
  if (loading) return <SkeletonRows />
  if (!groups.length) {
    return filtering ? (
      <EmptyState
        icon={SearchX}
        title="No events match"
        description="Try a wider time window or remove a filter."
        action={
          <Button size="sm" onClick={onClearFilters}>
            Clear filters
          </Button>
        }
      />
    ) : (
      <EmptyState icon={Activity} title="No events yet" description="Device, alert and admin activity will show up here as it happens." />
    )
  }

  const total = groups.reduce((a, g) => a + g.events.length, 0)

  return (
    <div className={cn('space-y-5 transition-opacity duration-300', fetching && 'opacity-80')}>
      {groups.map((g) => (
        <section key={g.key} aria-labelledby={`day-${g.key}`}>
          <div className="sticky top-[var(--topbar-h)] z-10 -mx-1 mb-2 flex items-center gap-3 bg-bg/90 px-1 py-1.5 backdrop-blur">
            <h2 id={`day-${g.key}`} className="text-xs font-semibold uppercase tracking-wider text-fg-secondary">
              {g.label}
            </h2>
            <span className="num text-[11px] text-fg-muted">{plural(g.events.length, 'event')}</span>
            <span className="h-px flex-1 bg-border" aria-hidden="true" />
          </div>
          <ul className="surface-card divide-y divide-border-subtle overflow-hidden">
            {g.events.map((e) => (
              <EventRow key={e.id} event={e} fresh={newIds.has(e.id)} now={now} />
            ))}
          </ul>
        </section>
      ))}
      <div ref={sentinel} className="flex flex-col items-center gap-2 py-2 text-xs text-fg-muted">
        {loadMoreError ? (
          <ErrorState compact error={loadMoreError} onRetry={onLoadMore} retrying={loadingMore} className="w-full max-w-md" />
        ) : hasMore ? (
          <Button size="sm" variant="outline" onClick={onLoadMore} loading={loadingMore} leadingIcon={ChevronDown}>
            Load older events
          </Button>
        ) : (
          <p className="num">
            Showing {formatInt(total)} {total === 1 ? 'event' : 'events'} · end of history
          </p>
        )}
      </div>
    </div>
  )
}

/** Floating "N new events" pill shown when live events arrive while scrolled down. */
export function NewEventsPill({ count, onClick }: { count: number; onClick: () => void }) {
  if (count <= 0) return null
  return (
    <div className="pointer-events-none fixed inset-x-0 top-[calc(var(--topbar-h)+12px)] z-30 flex justify-center px-4">
      <button
        type="button"
        onClick={onClick}
        className="pointer-events-auto inline-flex h-8 items-center gap-1.5 rounded-full bg-accent px-3.5 text-xs font-semibold text-accent-fg shadow-md animate-slide-up hover:bg-accent-hover focus-ring"
      >
        <ArrowUp className="size-3.5" aria-hidden="true" />
        {count} new {count === 1 ? 'event' : 'events'}
      </button>
    </div>
  )
}
