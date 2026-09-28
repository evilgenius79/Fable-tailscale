import { useCallback, useEffect, useMemo, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { Download } from 'lucide-react'
import { useDevices } from '../api/hooks'
import type { EventType } from '../api/types'
import { EventTimeline, NewEventsPill } from '../components/events/EventTimeline'
import { EventsToolbar } from '../components/events/EventsToolbar'
import {
  DEFAULT_EVENT_FILTERS,
  eventsToJSON,
  exportFilename,
  filterEvents,
  groupEventsByDay,
  hasEventFilters,
  parseEventFilters,
  serializeEventFilters,
  type EventFilters,
} from '../components/events/events'
import { useEventFeed, useNewEvents } from '../components/events/useEventFeed'
import { Button } from '../components/ui/Button'
import { PageHeader } from '../components/ui/PageHeader'
import { StatusDot } from '../components/ui/StatusDot'
import { toast } from '../components/ui/Toast'
import { formatInt, formatRelative, plural } from '../lib/format'
import { selectLive, useUIStore } from '../store'

/** Trigger a client-side download of a text blob (no external service involved). */
function downloadText(name: string, text: string, type = 'application/json') {
  const blob = new Blob([text], { type })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = name
  a.rel = 'noopener'
  document.body.appendChild(a)
  a.click()
  a.remove()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}

export default function EventsPage() {
  const [sp, setSp] = useSearchParams()
  const filters = useMemo(() => parseEventFilters(sp), [sp])
  const setFilters = useCallback(
    (patch: Partial<EventFilters>) => setSp((prev) => serializeEventFilters({ ...parseEventFilters(prev), ...patch }), { replace: true }),
    [setSp],
  )
  const reset = useCallback(() => setSp(new URLSearchParams(), { replace: true }), [setSp])

  const feed = useEventFeed(filters)
  const { newIds, markSeen } = useNewEvents(feed.headIds, !feed.isPending)
  const devices = useDevices()
  const live = useUIStore(selectLive)

  // One reference time per data change keeps relative labels consistent across rows.
  const now = useMemo(() => Date.now(), [feed.events])
  const groups = useMemo(() => groupEventsByDay(feed.events, new Date(now)), [feed.events, now])

  // Facet counts for the type picker: everything loaded that passes the other filters.
  const typeCounts = useMemo(() => {
    const base = filterEvents(feed.loaded, { ...filters, types: [] }, now)
    const out: Partial<Record<EventType, number>> = {}
    for (const e of base) out[e.type] = (out[e.type] ?? 0) + 1
    return out
  }, [feed.loaded, filters, now])

  // Device options: every known device plus any device an event references.
  const deviceOptions = useMemo(() => {
    const map = new Map<string, string>()
    for (const d of devices.data ?? []) map.set(d.id, d.name)
    for (const e of feed.loaded) if (e.deviceId && !map.has(e.deviceId)) map.set(e.deviceId, e.deviceName ?? e.deviceId)
    if (filters.device && !map.has(filters.device)) map.set(filters.device, filters.device)
    return Array.from(map, ([value, label]) => ({ value, label })).sort((a, b) => a.label.localeCompare(b.label))
  }, [devices.data, feed.loaded, filters.device])

  // "New events" pill: only when the reader has scrolled away from the top.
  const [scrolled, setScrolled] = useState(false)
  useEffect(() => {
    const onScroll = () => setScrolled(window.scrollY > 240)
    onScroll()
    window.addEventListener('scroll', onScroll, { passive: true })
    return () => window.removeEventListener('scroll', onScroll)
  }, [])
  useEffect(() => {
    if (scrolled || !newIds.size) return
    const t = setTimeout(markSeen, 6000)
    return () => clearTimeout(t)
  }, [scrolled, newIds, markSeen])
  const jumpToTop = () => window.scrollTo({ top: 0, behavior: 'smooth' })

  const filtering = hasEventFilters(filters)
  const visibleNew = feed.events.filter((e) => newIds.has(e.id)).length

  const onExport = () => {
    const json = eventsToJSON(feed.events, { exportedAt: new Date().toISOString(), filters })
    downloadText(exportFilename(), json)
    toast.success('Events exported', `${plural(feed.events.length, 'event')} saved as JSON.`, { duration: 2500 })
  }

  const newest = feed.events[0]

  return (
    <>
      <PageHeader
        title="Events"
        description="A live, filterable timeline of everything that happened on the tailnet."
        meta={
          <>
            <span className="inline-flex items-center gap-1.5">
              <StatusDot tone={live.state === 'connected' ? 'online' : live.state === 'polling' ? 'warning' : 'neutral'} pulse={live.state === 'connected'} size="xs" />
              {live.state === 'connected' ? 'Streaming live' : live.state === 'polling' ? 'Polling every 15s' : 'Connecting…'}
            </span>
            {!feed.isPending ? (
              <>
                <span aria-hidden="true">·</span>
                <span className="num">{formatInt(feed.loaded.length)} loaded</span>
              </>
            ) : null}
            {newest ? (
              <>
                <span aria-hidden="true">·</span>
                <span className="num">newest {formatRelative(newest.ts, now)}</span>
              </>
            ) : null}
          </>
        }
        actions={
          <Button size="sm" leadingIcon={Download} onClick={onExport} disabled={feed.isPending || feed.events.length === 0} aria-label="Export visible events as JSON">
            Export JSON
          </Button>
        }
      >
        <EventsToolbar
          filters={filters}
          onChange={setFilters}
          onReset={reset}
          devices={deviceOptions}
          typeCounts={typeCounts}
          summary={
            <p className="num" aria-live="polite">
              {feed.isPending ? (
                'Loading events…'
              ) : filtering ? (
                <>
                  <span className="font-medium text-fg">{formatInt(feed.events.length)}</span> of {plural(feed.loaded.length, 'loaded event')} match
                </>
              ) : (
                <span className="font-medium text-fg">{plural(feed.events.length, 'event')}</span>
              )}
            </p>
          }
        />
      </PageHeader>

      <EventTimeline
        groups={groups}
        loading={feed.isPending}
        fetching={feed.isFetching}
        error={feed.error}
        onRetry={feed.refetch}
        hasMore={feed.hasMore}
        loadingMore={feed.loadingMore}
        loadMoreError={feed.loadMoreError}
        onLoadMore={feed.loadMore}
        filtering={filtering}
        onClearFilters={() => setFilters({ ...DEFAULT_EVENT_FILTERS, since: 'all' })}
        newIds={newIds}
        now={now}
      />
      <NewEventsPill count={scrolled ? visibleNew : 0} onClick={jumpToTop} />
    </>
  )
}
