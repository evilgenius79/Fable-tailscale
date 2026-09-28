// TanStack Query hooks for every endpoint in docs/API.md.
//
// Live data (overview, devices, device detail, events, alerts) is pushed over
// SSE by useLiveStream() and written straight into these caches, so their
// refetchInterval is only enabled in the polling fallback.

import { keepPreviousData, useMutation, useQuery, useQueryClient, type UseQueryOptions } from '@tanstack/react-query'
import { apiDelete, apiGet, apiPost, apiPut, seg } from './client'
import { queryKeys, type AlertsParams, type EventsParams } from './queryKeys'
import type {
  Alert,
  AlertRule,
  AuditEntry,
  Device,
  DeviceDetail,
  DeviceID,
  Event,
  Identity,
  Overview,
  PingResult,
  Series,
  Settings,
  Topology,
  UptimeReport,
} from './types'
import { useUIStore, selectIsPolling } from '../store'
import { rangeRefetchMs, type RangeKey } from '../lib/time'

export const POLL_INTERVAL_MS = 15_000

/** refetchInterval for live collections: 15s only while the SSE stream is down. */
export function useLiveRefetchInterval(): number | false {
  const polling = useUIStore(selectIsPolling)
  return polling ? POLL_INTERVAL_MS : false
}

// ---------------------------------------------------------------------------
// Queries
// ---------------------------------------------------------------------------

export function useMe() {
  return useQuery<Identity>({
    queryKey: queryKeys.me,
    queryFn: ({ signal }) => apiGet<Identity>('/me', { signal }),
    staleTime: 5 * 60_000,
    retry: (count, err) => !(err as { status?: number }).status || ((err as { status?: number }).status ?? 0) >= 500 ? count < 2 : false,
  })
}

/** Convenience: true when the current identity is an admin. */
export function useIsAdmin(): boolean {
  const { data } = useMe()
  return data?.role === 'admin'
}

export function useOverview() {
  const refetchInterval = useLiveRefetchInterval()
  return useQuery<Overview>({
    queryKey: queryKeys.overview,
    queryFn: ({ signal }) => apiGet<Overview>('/overview', { signal }),
    staleTime: 15_000,
    refetchInterval,
  })
}

/** Open-alert count for the nav badge (from the overview). */
export function useOpenAlertCount(): number {
  const { data } = useOverview()
  return data?.openAlerts ?? 0
}

export function useSettings() {
  return useQuery<Settings>({
    queryKey: queryKeys.settings,
    queryFn: ({ signal }) => apiGet<Settings>('/settings', { signal }),
    staleTime: 60_000,
  })
}

export function useDevices() {
  const refetchInterval = useLiveRefetchInterval()
  return useQuery<Device[]>({
    queryKey: queryKeys.devices,
    queryFn: ({ signal }) => apiGet<Device[]>('/devices', { signal }),
    staleTime: 15_000,
    refetchInterval,
  })
}

export function useDevice(id: DeviceID | undefined) {
  const refetchInterval = useLiveRefetchInterval()
  return useQuery<DeviceDetail>({
    queryKey: queryKeys.device(id ?? ''),
    queryFn: ({ signal }) => apiGet<DeviceDetail>(`/devices/${seg(id ?? '')}`, { signal }),
    enabled: !!id,
    staleTime: 15_000,
    refetchInterval,
    retry: (count, err) => ((err as { status?: number }).status === 404 ? false : count < 2),
  })
}

export function useSeries(id: DeviceID | undefined, range: RangeKey, options?: Pick<UseQueryOptions<Series>, 'enabled'>) {
  return useQuery<Series>({
    queryKey: queryKeys.deviceSeries(id ?? '', range),
    queryFn: ({ signal }) => apiGet<Series>(`/devices/${seg(id ?? '')}/series`, { params: { range }, signal }),
    enabled: !!id && (options?.enabled ?? true),
    staleTime: 20_000,
    refetchInterval: rangeRefetchMs(range),
    placeholderData: keepPreviousData,
  })
}

export function useUptime(id: DeviceID | undefined, range: RangeKey, options?: Pick<UseQueryOptions<UptimeReport>, 'enabled'>) {
  return useQuery<UptimeReport>({
    queryKey: queryKeys.deviceUptime(id ?? '', range),
    queryFn: ({ signal }) => apiGet<UptimeReport>(`/devices/${seg(id ?? '')}/uptime`, { params: { range }, signal }),
    enabled: !!id && (options?.enabled ?? true),
    staleTime: 30_000,
    refetchInterval: 60_000,
    placeholderData: keepPreviousData,
  })
}

export function useDeviceEvents(id: DeviceID | undefined, limit = 50) {
  const refetchInterval = useLiveRefetchInterval()
  return useQuery<Event[]>({
    queryKey: queryKeys.deviceEvents(id ?? '', limit),
    queryFn: ({ signal }) => apiGet<Event[]>(`/devices/${seg(id ?? '')}/events`, { params: { limit }, signal }),
    enabled: !!id,
    staleTime: 15_000,
    refetchInterval,
  })
}

export function useEvents(params: EventsParams = {}) {
  const refetchInterval = useLiveRefetchInterval()
  const p: EventsParams = { limit: 100, ...params }
  return useQuery<Event[]>({
    queryKey: queryKeys.events(p),
    queryFn: ({ signal }) => apiGet<Event[]>('/events', { params: { ...p }, signal }),
    staleTime: 15_000,
    refetchInterval,
    placeholderData: keepPreviousData,
  })
}

export function useAlerts(params: AlertsParams = {}) {
  const refetchInterval = useLiveRefetchInterval()
  const p: AlertsParams = { state: 'open', limit: 200, ...params }
  return useQuery<Alert[]>({
    queryKey: queryKeys.alerts(p),
    queryFn: ({ signal }) => apiGet<Alert[]>('/alerts', { params: { ...p }, signal }),
    staleTime: 15_000,
    refetchInterval,
    placeholderData: keepPreviousData,
  })
}

export function useRules() {
  return useQuery<AlertRule[]>({
    queryKey: queryKeys.rules,
    queryFn: ({ signal }) => apiGet<AlertRule[]>('/alerts/rules', { signal }),
    staleTime: 60_000,
  })
}

export function useTopology() {
  const polling = useUIStore(selectIsPolling)
  return useQuery<Topology>({
    queryKey: queryKeys.topology,
    queryFn: ({ signal }) => apiGet<Topology>('/network/topology', { signal }),
    staleTime: 30_000,
    refetchInterval: polling ? POLL_INTERVAL_MS : 60_000,
    placeholderData: keepPreviousData,
  })
}

/** Admin only — pass `enabled: false` for viewers to avoid a 403. */
export function useAudit(limit = 100, options?: { enabled?: boolean }) {
  return useQuery<AuditEntry[]>({
    queryKey: queryKeys.audit(limit),
    queryFn: ({ signal }) => apiGet<AuditEntry[]>('/audit', { params: { limit }, signal }),
    enabled: options?.enabled ?? true,
    staleTime: 30_000,
    retry: false,
  })
}

// ---------------------------------------------------------------------------
// Mutations
// ---------------------------------------------------------------------------

function useDeviceMutationEffects() {
  const qc = useQueryClient()
  return {
    applyDevice(updated: Device) {
      qc.setQueryData<Device[]>(queryKeys.devices, (list) => (list ? list.map((d) => (d.id === updated.id ? updated : d)) : list))
      qc.setQueryData<DeviceDetail>(queryKeys.device(updated.id), (detail) => (detail ? { ...detail, device: updated } : detail))
      void qc.invalidateQueries({ queryKey: queryKeys.overview })
      void qc.invalidateQueries({ queryKey: queryKeys.eventsAll })
      void qc.invalidateQueries({ queryKey: queryKeys.auditAll })
      void qc.invalidateQueries({ queryKey: queryKeys.topology })
    },
    removeDevice(id: DeviceID) {
      qc.setQueryData<Device[]>(queryKeys.devices, (list) => (list ? list.filter((d) => d.id !== id) : list))
      qc.removeQueries({ queryKey: queryKeys.device(id) })
      void qc.invalidateQueries({ queryKey: queryKeys.overview })
      void qc.invalidateQueries({ queryKey: queryKeys.eventsAll })
      void qc.invalidateQueries({ queryKey: queryKeys.auditAll })
      void qc.invalidateQueries({ queryKey: queryKeys.topology })
    },
  }
}

export function usePing() {
  const qc = useQueryClient()
  return useMutation<PingResult, Error, DeviceID>({
    mutationFn: (id) => apiPost<PingResult>(`/devices/${seg(id)}/ping`),
    onSuccess: (_res, id) => {
      void qc.invalidateQueries({ queryKey: queryKeys.device(id) })
      void qc.invalidateQueries({ queryKey: queryKeys.auditAll })
    },
  })
}

export function useAuthorize() {
  const fx = useDeviceMutationEffects()
  return useMutation<Device, Error, { id: DeviceID; authorized?: boolean }>({
    mutationFn: ({ id, authorized = true }) => apiPost<Device>(`/devices/${seg(id)}/authorize`, { authorized }),
    onSuccess: (d) => fx.applyDevice(d),
  })
}

export function useSetTags() {
  const fx = useDeviceMutationEffects()
  return useMutation<Device, Error, { id: DeviceID; tags: string[] }>({
    mutationFn: ({ id, tags }) => apiPost<Device>(`/devices/${seg(id)}/tags`, { tags }),
    onSuccess: (d) => fx.applyDevice(d),
  })
}

export function useSetKeyExpiry() {
  const fx = useDeviceMutationEffects()
  return useMutation<Device, Error, { id: DeviceID; disabled: boolean }>({
    mutationFn: ({ id, disabled }) => apiPost<Device>(`/devices/${seg(id)}/key-expiry`, { disabled }),
    onSuccess: (d) => fx.applyDevice(d),
  })
}

export function useSetRoutes() {
  const fx = useDeviceMutationEffects()
  return useMutation<Device, Error, { id: DeviceID; routes: string[] }>({
    mutationFn: ({ id, routes }) => apiPost<Device>(`/devices/${seg(id)}/routes`, { routes }),
    onSuccess: (d) => fx.applyDevice(d),
  })
}

export function useRename() {
  const fx = useDeviceMutationEffects()
  return useMutation<Device, Error, { id: DeviceID; name: string }>({
    mutationFn: ({ id, name }) => apiPost<Device>(`/devices/${seg(id)}/name`, { name }),
    onSuccess: (d) => fx.applyDevice(d),
  })
}

export function useDeleteDevice() {
  const fx = useDeviceMutationEffects()
  return useMutation<void, Error, DeviceID>({
    mutationFn: (id) => apiDelete<void>(`/devices/${seg(id)}`),
    onSuccess: (_v, id) => fx.removeDevice(id),
  })
}

export function useAckAlert() {
  const qc = useQueryClient()
  return useMutation<Alert, Error, number>({
    mutationFn: (id) => apiPost<Alert>(`/alerts/${seg(id)}/ack`),
    onSuccess: (a) => {
      qc.setQueriesData<Alert[]>({ queryKey: queryKeys.alertsAll }, (list) => (Array.isArray(list) ? list.map((x) => (x.id === a.id ? a : x)) : list))
      if (a.deviceId) {
        qc.setQueryData<DeviceDetail>(queryKeys.device(a.deviceId), (detail) =>
          detail ? { ...detail, openAlerts: detail.openAlerts.map((x) => (x.id === a.id ? a : x)) } : detail,
        )
      }
      void qc.invalidateQueries({ queryKey: queryKeys.alertsAll })
      void qc.invalidateQueries({ queryKey: queryKeys.eventsAll })
      void qc.invalidateQueries({ queryKey: queryKeys.auditAll })
    },
  })
}

export function useSaveRule() {
  const qc = useQueryClient()
  return useMutation<AlertRule, Error, AlertRule>({
    mutationFn: (rule) => apiPut<AlertRule>(`/alerts/rules/${seg(rule.id)}`, rule),
    onSuccess: (r) => {
      qc.setQueryData<AlertRule[]>(queryKeys.rules, (list) => (list ? list.map((x) => (x.id === r.id ? r : x)) : list))
      void qc.invalidateQueries({ queryKey: queryKeys.rules })
      void qc.invalidateQueries({ queryKey: queryKeys.auditAll })
    },
  })
}

export interface TestNotificationResult {
  sent: string[]
  errors: Record<string, string>
}

export function useTestNotification() {
  return useMutation<TestNotificationResult, Error, { message: string }>({
    mutationFn: (body) => apiPost<TestNotificationResult>('/alerts/test', body),
  })
}

export function useRefresh() {
  const qc = useQueryClient()
  return useMutation<void, Error, void>({
    mutationFn: () => apiPost<void>('/refresh'),
    onSuccess: () => {
      // The collector polls asynchronously; give it a moment then refetch.
      setTimeout(() => {
        void qc.invalidateQueries({ queryKey: queryKeys.overview })
        void qc.invalidateQueries({ queryKey: queryKeys.devicesAll })
        void qc.invalidateQueries({ queryKey: queryKeys.topology })
      }, 1500)
    },
  })
}
