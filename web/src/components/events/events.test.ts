import { describe, expect, it } from 'vitest'
import type { Event } from '../../api/types'
import {
  ALL_EVENT_TYPES,
  DEFAULT_EVENT_FILTERS,
  EVENT_GROUPS,
  countActiveEventFilters,
  eventGroupOf,
  eventQueryParams,
  eventsToJSON,
  exportFilename,
  filterEvents,
  groupEventsByDay,
  hasEventFilters,
  mergeEvents,
  newIds,
  oldestTs,
  parseEventFilters,
  serializeEventFilters,
  sinceISO,
} from './events'

const ev = (over: Partial<Event> = {}): Event => ({
  id: 1,
  ts: '2026-09-28T10:00:00.000Z',
  type: 'device.online',
  severity: 'info',
  deviceId: 'd1',
  deviceName: 'nas',
  title: 'nas came online',
  ...over,
})

describe('groups', () => {
  it('covers all 16 event types once', () => {
    expect(ALL_EVENT_TYPES.length).toBe(16)
    expect(new Set(ALL_EVENT_TYPES).size).toBe(16)
    expect(EVENT_GROUPS.map((g) => g.id)).toEqual(['device', 'agent', 'alert', 'admin', 'hub'])
  })
  it('maps a type to its group', () => {
    expect(eventGroupOf('device.path_changed')).toBe('device')
    expect(eventGroupOf('alert.acked')).toBe('alert')
    expect(eventGroupOf('hub.error')).toBe('hub')
  })
})

describe('filters ↔ URL', () => {
  it('parses defaults from an empty query', () => {
    expect(parseEventFilters(new URLSearchParams())).toEqual(DEFAULT_EVENT_FILTERS)
    expect(hasEventFilters(DEFAULT_EVENT_FILTERS)).toBe(false)
    expect(countActiveEventFilters(DEFAULT_EVENT_FILTERS)).toBe(0)
  })
  it('round-trips and drops unknown values', () => {
    const f = { q: 'nas', types: ['device.offline', 'hub.error'] as Event['type'][], severity: 'warning' as const, device: 'd1', since: 'all' as const }
    const sp = serializeEventFilters(f)
    expect(sp.get('type')).toBe('device.offline,hub.error')
    expect(parseEventFilters(sp)).toEqual(f)
    const bad = parseEventFilters(new URLSearchParams('type=nope,device.new,device.new&severity=loud&since=1y'))
    expect(bad.types).toEqual(['device.new'])
    expect(bad.severity).toBe('all')
    expect(bad.since).toBe('24h')
    expect(countActiveEventFilters(f)).toBe(4)
  })
  it('omits defaults when serialising', () => {
    expect(serializeEventFilters(DEFAULT_EVENT_FILTERS).toString()).toBe('')
  })
})

describe('server params', () => {
  const now = Date.parse('2026-09-28T12:00:00Z')
  it('passes a single type through and keeps multiple client-side', () => {
    expect(eventQueryParams({ ...DEFAULT_EVENT_FILTERS, types: ['hub.error'] }, 100, now)).toEqual({ limit: 100, type: 'hub.error', since: '2026-09-27T12:00:00.000Z' })
    expect(eventQueryParams({ ...DEFAULT_EVENT_FILTERS, types: ['hub.error', 'hub.started'], since: 'all' }, 50, now)).toEqual({ limit: 50 })
    expect(eventQueryParams({ ...DEFAULT_EVENT_FILTERS, device: 'd1', since: '1h' }, 100, now).device).toBe('d1')
  })
  it('computes since bounds', () => {
    expect(sinceISO('1h', now)).toBe('2026-09-28T11:00:00.000Z')
    expect(sinceISO('7d', now)).toBe('2026-09-21T12:00:00.000Z')
    expect(sinceISO('all', now)).toBeUndefined()
  })
})

describe('filterEvents', () => {
  const now = Date.parse('2026-09-28T12:00:00Z')
  const list: Event[] = [
    ev({ id: 1, ts: '2026-09-28T11:30:00.000Z', type: 'device.offline', severity: 'warning' }),
    ev({ id: 2, ts: '2026-09-28T09:00:00.000Z', type: 'hub.error', severity: 'warning', deviceId: undefined, deviceName: undefined, title: 'Control API poll failed', message: '502 Bad Gateway' }),
    ev({ id: 3, ts: '2026-09-20T09:00:00.000Z', type: 'device.new', deviceId: 'd2', deviceName: 'pi', title: 'pi joined' }),
  ]
  it('applies every dimension', () => {
    expect(filterEvents(list, { ...DEFAULT_EVENT_FILTERS, since: 'all' }, now).map((e) => e.id)).toEqual([1, 2, 3])
    expect(filterEvents(list, { ...DEFAULT_EVENT_FILTERS, since: '1h' }, now).map((e) => e.id)).toEqual([1])
    expect(filterEvents(list, { ...DEFAULT_EVENT_FILTERS, since: 'all', types: ['hub.error', 'device.new'] }, now).map((e) => e.id)).toEqual([2, 3])
    expect(filterEvents(list, { ...DEFAULT_EVENT_FILTERS, since: 'all', severity: 'warning' }, now).map((e) => e.id)).toEqual([1, 2])
    expect(filterEvents(list, { ...DEFAULT_EVENT_FILTERS, since: 'all', device: 'd2' }, now).map((e) => e.id)).toEqual([3])
    expect(filterEvents(list, { ...DEFAULT_EVENT_FILTERS, since: 'all', q: 'gateway' }, now).map((e) => e.id)).toEqual([2])
    expect(filterEvents(list, { ...DEFAULT_EVENT_FILTERS, since: 'all', q: 'JOINED' }, now).map((e) => e.id)).toEqual([3])
  })
})

describe('pages', () => {
  it('merges, dedupes and sorts newest first', () => {
    const a = [ev({ id: 5, ts: '2026-09-28T10:05:00.000Z' }), ev({ id: 4, ts: '2026-09-28T10:04:00.000Z' })]
    const b = [ev({ id: 4, ts: '2026-09-28T10:04:00.000Z', title: 'dup' }), ev({ id: 9, ts: '2026-09-28T10:09:00.000Z' }), ev({ id: 1, ts: '2026-09-28T10:00:00.000Z' })]
    const merged = mergeEvents(a, b)
    expect(merged.map((e) => e.id)).toEqual([9, 5, 4, 1])
    expect(merged.find((e) => e.id === 4)!.title).toBe('nas came online')
  })
  it('breaks ts ties by id', () => {
    const same = [ev({ id: 1 }), ev({ id: 3 }), ev({ id: 2 })]
    expect(mergeEvents(same).map((e) => e.id)).toEqual([3, 2, 1])
  })
  it('finds new ids and the oldest cursor', () => {
    const page = [ev({ id: 1 }), ev({ id: 2, ts: '2026-09-27T10:00:00.000Z' })]
    expect(newIds(page, new Set([1]))).toEqual([2])
    expect(oldestTs(page)).toBe('2026-09-27T10:00:00.000Z')
    expect(oldestTs([])).toBeUndefined()
  })
})

describe('groupEventsByDay', () => {
  it('groups consecutive events by local day with labels', () => {
    const now = new Date('2026-09-28T15:00:00')
    const list = [
      ev({ id: 3, ts: new Date('2026-09-28T14:00:00').toISOString() }),
      ev({ id: 2, ts: new Date('2026-09-28T01:00:00').toISOString() }),
      ev({ id: 1, ts: new Date('2026-09-27T23:00:00').toISOString() }),
      ev({ id: 0, ts: new Date('2026-09-20T23:00:00').toISOString() }),
    ]
    const groups = groupEventsByDay(list, now)
    expect(groups.map((g) => [g.label, g.events.length])).toEqual([
      ['Today', 2],
      ['Yesterday', 1],
      ['Sun, Sep 20', 1],
    ])
  })
  it('handles an empty list', () => {
    expect(groupEventsByDay([])).toEqual([])
  })
})

describe('export', () => {
  it('serialises events with metadata', () => {
    const json = eventsToJSON([ev()], { exportedAt: '2026-09-28T12:00:00.000Z', filters: DEFAULT_EVENT_FILTERS })
    const parsed = JSON.parse(json) as { count: number; events: Event[]; exportedAt: string }
    expect(parsed.count).toBe(1)
    expect(parsed.events[0]!.title).toBe('nas came online')
    expect(parsed.exportedAt).toBe('2026-09-28T12:00:00.000Z')
  })
  it('builds a filesystem-safe filename', () => {
    expect(exportFilename(Date.parse('2026-09-28T12:05:07.123Z'))).toBe('tailwatch-events-2026-09-28T12-05-07.json')
  })
})
