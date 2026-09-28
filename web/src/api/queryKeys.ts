import type { RangeKey } from '../lib/time'

export interface EventsParams {
  limit?: number
  since?: string
  type?: string
  device?: string
}

export interface AlertsParams {
  state?: 'open' | 'resolved' | 'all'
  device?: string
  limit?: number
}

/**
 * Query keys. Prefix keys (`devicesAll`, `eventsAll`, …) are for invalidation;
 * the leaf builders are what hooks use. Keep every key JSON-serialisable.
 */
export const queryKeys = {
  me: ['me'] as const,
  overview: ['overview'] as const,
  settings: ['settings'] as const,

  devicesAll: ['devices'] as const,
  devices: ['devices', 'list'] as const,
  device: (id: string) => ['devices', 'detail', id] as const,
  deviceSeries: (id: string, range: RangeKey) => ['devices', 'series', id, range] as const,
  deviceSeriesAll: (id: string) => ['devices', 'series', id] as const,
  deviceUptime: (id: string, range: RangeKey) => ['devices', 'uptime', id, range] as const,
  deviceEvents: (id: string, limit: number) => ['devices', 'events', id, limit] as const,
  deviceEventsAll: (id: string) => ['devices', 'events', id] as const,

  eventsAll: ['events'] as const,
  events: (params: EventsParams) => ['events', 'list', params] as const,

  alertsAll: ['alerts'] as const,
  alerts: (params: AlertsParams) => ['alerts', 'list', params] as const,
  rules: ['alerts', 'rules'] as const,

  topology: ['topology'] as const,
  auditAll: ['audit'] as const,
  audit: (limit: number) => ['audit', limit] as const,
}

export type QueryKeys = typeof queryKeys
