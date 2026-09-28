// Paginated, live event feed.
//
// The newest page comes from `useEvents()` (kept fresh by the SSE stream and
// the polling fallback). Older pages are fetched on demand with a `before`
// cursor (the oldest timestamp loaded so far) and merged client-side; pages
// are deduplicated by id so a server that ignores `before` simply reports
// "no more events" instead of looping.

import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { apiGet } from '../../api/client'
import { useEvents } from '../../api/hooks'
import type { EventsParams } from '../../api/queryKeys'
import type { Event } from '../../api/types'
import { eventQueryParams, filterEvents, mergeEvents, oldestTs, type EventFilters } from './events'

export const EVENT_PAGE_SIZE = 100

export interface EventFeed {
  /** Every loaded event after client-side filtering, newest first. */
  events: Event[]
  /** Everything loaded (head + older pages) before client-side filtering. */
  loaded: Event[]
  isPending: boolean
  isFetching: boolean
  error: unknown
  refetch: () => void
  hasMore: boolean
  loadingMore: boolean
  loadMoreError: unknown
  loadMore: () => void
  /** Ids in the head page that arrived after the feed was first seen. */
  headIds: number[]
}

export function useEventFeed(filters: EventFilters, pageSize = EVENT_PAGE_SIZE): EventFeed {
  const typesKey = filters.types.join(',')
  // `since` is an absolute timestamp, so compute it once per filter change to keep the query key stable.
  const { since, device } = filters
  const params = useMemo<EventsParams>(
    () => eventQueryParams({ q: '', severity: 'all', since, device, types: typesKey ? (typesKey.split(',') as EventFilters['types']) : [] }, pageSize, Date.now()),
    [since, device, typesKey, pageSize],
  )
  const head = useEvents(params)

  const [older, setOlder] = useState<Event[]>([])
  const [exhausted, setExhausted] = useState(false)
  const [loadingMore, setLoadingMore] = useState(false)
  const [loadMoreError, setLoadMoreError] = useState<unknown>(null)
  const abortRef = useRef<AbortController | null>(null)

  // New server query → forget older pages.
  useEffect(() => {
    setOlder([])
    setExhausted(false)
    setLoadMoreError(null)
    abortRef.current?.abort()
    abortRef.current = null
    setLoadingMore(false)
  }, [params])

  useEffect(() => () => abortRef.current?.abort(), [])

  const merged = useMemo(() => mergeEvents(head.data ?? [], older), [head.data, older])
  const events = useMemo(() => filterEvents(merged, filters), [merged, filters])

  const headFull = (head.data?.length ?? 0) >= pageSize
  const hasMore = !exhausted && headFull

  const loadMore = useCallback(() => {
    if (loadingMore || !hasMore) return
    const cursor = oldestTs(merged)
    if (!cursor) return
    const known = new Set(merged.map((e) => e.id))
    const ac = new AbortController()
    abortRef.current = ac
    setLoadingMore(true)
    setLoadMoreError(null)
    apiGet<Event[]>('/events', { params: { ...params, before: cursor }, signal: ac.signal })
      .then((page) => {
        if (ac.signal.aborted) return
        const fresh = (Array.isArray(page) ? page : []).filter((e) => !known.has(e.id))
        if (!fresh.length || page.length < pageSize) setExhausted(true)
        if (fresh.length) setOlder((prev) => mergeEvents(prev, fresh))
      })
      .catch((e: unknown) => {
        if (ac.signal.aborted) return
        setLoadMoreError(e)
      })
      .finally(() => {
        if (abortRef.current === ac) abortRef.current = null
        if (!ac.signal.aborted) setLoadingMore(false)
      })
  }, [loadingMore, hasMore, merged, params, pageSize])

  return {
    events,
    loaded: merged,
    isPending: head.isPending,
    isFetching: head.isFetching && !head.isPending,
    error: head.error,
    refetch: () => void head.refetch(),
    hasMore,
    loadingMore,
    loadMoreError,
    loadMore,
    headIds: useMemo(() => (head.data ?? []).map((e) => e.id), [head.data]),
  }
}

/**
 * Track which head-page ids are "new" (arrived after the reader last looked).
 * `markSeen()` clears the set; ids are considered seen automatically on first load.
 */
export function useNewEvents(headIds: number[], ready: boolean): { newIds: Set<number>; markSeen: () => void } {
  const seenRef = useRef<Set<number> | null>(null)
  const [newIds, setNewIds] = useState<Set<number>>(() => new Set())

  useEffect(() => {
    if (!ready) return
    if (seenRef.current === null) {
      seenRef.current = new Set(headIds)
      return
    }
    const seen = seenRef.current
    const fresh = headIds.filter((id) => !seen.has(id))
    if (!fresh.length) return
    setNewIds((prev) => {
      const next = new Set(prev)
      for (const id of fresh) next.add(id)
      return next
    })
  }, [headIds, ready])

  const markSeen = useCallback(() => {
    seenRef.current = new Set(headIds)
    setNewIds((prev) => (prev.size ? new Set() : prev))
  }, [headIds])

  return { newIds, markSeen }
}
