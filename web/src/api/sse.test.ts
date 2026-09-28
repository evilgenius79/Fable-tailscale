import { beforeEach, describe, expect, it, vi } from 'vitest'
import { QueryClient } from '@tanstack/react-query'
import { applyAlert, applyEvent, applyTick, eventMatches, markLocalAck, toastForAlert } from './sse'
import { toast } from '../components/ui/Toast'
import { queryKeys } from './queryKeys'
import { createMockState, overview } from './mockData'
import type { Alert, Device, DeviceDetail, Event } from './types'

function seed() {
  const state = createMockState(Date.parse('2026-09-28T12:00:00Z'))
  const qc = new QueryClient()
  return { state, qc }
}

describe('applyTick', () => {
  it('writes overview, sorted devices and patches detail caches', () => {
    const { state, qc } = seed()
    const dev = state.devices[3]!
    qc.setQueryData<DeviceDetail>(queryKeys.device(dev.id), { device: { ...dev, online: false }, recentEvents: [], openAlerts: [] })
    const ov = overview(state, Date.now())
    applyTick(qc, { overview: ov, devices: [...state.devices].reverse() })
    expect(qc.getQueryData(queryKeys.overview)).toBe(ov)
    const list = qc.getQueryData<Device[]>(queryKeys.devices)!
    expect(list.map((d) => d.name)).toEqual([...list].map((d) => d.name).sort((a, b) => a.localeCompare(b)))
    expect(qc.getQueryData<DeviceDetail>(queryKeys.device(dev.id))!.device.online).toBe(dev.online)
    // untouched caches are not created
    expect(qc.getQueryData(queryKeys.device(state.devices[0]!.id))).toBeUndefined()
  })
})

describe('applyEvent', () => {
  it('prepends to matching lists only, dedupes and respects limit', () => {
    const { state, qc } = seed()
    const dev = state.devices[1]!
    const e: Event = { id: 424242, ts: new Date().toISOString(), type: 'device.offline', severity: 'warning', deviceId: dev.id, deviceName: dev.name, title: 'x' }
    qc.setQueryData<Event[]>(queryKeys.events({ limit: 2 }), [{ ...e, id: 1 }, { ...e, id: 2 }])
    qc.setQueryData<Event[]>(queryKeys.events({ limit: 100, type: 'device.online' }), [])
    qc.setQueryData<Event[]>(queryKeys.events({ limit: 100, device: 'other' }), [])
    qc.setQueryData<Event[]>(queryKeys.deviceEvents(dev.id, 50), [])
    qc.setQueryData<DeviceDetail>(queryKeys.device(dev.id), { device: dev, recentEvents: [], openAlerts: [] })
    applyEvent(qc, e)
    applyEvent(qc, e)
    expect(qc.getQueryData<Event[]>(queryKeys.events({ limit: 2 }))!.map((x) => x.id)).toEqual([424242, 1])
    expect(qc.getQueryData<Event[]>(queryKeys.events({ limit: 100, type: 'device.online' }))).toEqual([])
    expect(qc.getQueryData<Event[]>(queryKeys.events({ limit: 100, device: 'other' }))).toEqual([])
    expect(qc.getQueryData<Event[]>(queryKeys.deviceEvents(dev.id, 50))!.length).toBe(1)
    expect(qc.getQueryData<DeviceDetail>(queryKeys.device(dev.id))!.recentEvents[0]!.id).toBe(424242)
  })
})

describe('eventMatches', () => {
  it('treats the type param as a comma-separated list', () => {
    const e = { type: 'device.online', deviceId: 'n1' } as Event
    expect(eventMatches({ type: 'device.offline,device.online' }, e)).toBe(true)
    expect(eventMatches({ type: 'device.offline' }, e)).toBe(false)
    expect(eventMatches({ type: 'device.online', device: 'other' }, e)).toBe(false)
    expect(eventMatches({}, e)).toBe(true)
  })
})

describe('applyAlert', () => {
  it('updates cached alerts and the device detail open list', () => {
    const { state, qc } = seed()
    const a = state.alerts[0]!
    qc.setQueryData<Alert[]>(queryKeys.alerts({ state: 'open', limit: 200 }), [a])
    qc.setQueryData<DeviceDetail>(queryKeys.device(a.deviceId!), { device: state.devices[0]!, recentEvents: [], openAlerts: [a] })
    const rules = state.rules
    qc.setQueryData(queryKeys.rules, rules)
    const resolved: Alert = { ...a, state: 'resolved', resolvedAt: new Date().toISOString() }
    applyAlert(qc, resolved)
    expect(qc.getQueryData<Alert[]>(queryKeys.alerts({ state: 'open', limit: 200 }))![0]!.state).toBe('resolved')
    expect(qc.getQueryData<DeviceDetail>(queryKeys.device(a.deviceId!))!.openAlerts).toEqual([])
    // The rules cache is not under the alerts prefix: untouched and not invalidated.
    expect(qc.getQueryData(queryKeys.rules)).toBe(rules)
    expect(qc.getQueryState(queryKeys.rules)?.isInvalidated).toBe(false)
    expect(qc.getQueryState(queryKeys.alerts({ state: 'open', limit: 200 }))?.isInvalidated).toBe(true)
  })
})

vi.mock('../components/ui/Toast', () => ({ toast: Object.assign(vi.fn(), { success: vi.fn(), info: vi.fn(), warning: vi.fn(), error: vi.fn() }) }))

describe('toastForAlert', () => {
  const base = { id: 7, title: 'High CPU on x', severity: 'warning', state: 'open', message: 'm' } as Alert
  beforeEach(() => vi.mocked(toast).mockClear())

  it('skips the stream echo of an ack this client made, once', () => {
    markLocalAck(7)
    toastForAlert({ ...base, ackedAt: '2026-09-28T12:00:00Z', ackedBy: 'me' })
    expect(toast).not.toHaveBeenCalled()
    // A later ack of the same alert (e.g. by someone else after a re-open) is shown again.
    toastForAlert({ ...base, ackedAt: '2026-09-28T12:05:00Z', ackedBy: 'them' })
    expect(toast).toHaveBeenCalledTimes(1)
    expect(vi.mocked(toast).mock.calls[0]![0]).toMatchObject({ id: 'alert-7', tone: 'info', title: 'Acknowledged: High CPU on x', description: 'by them' })
  })

  it('toasts acks made elsewhere and resolutions, keyed per alert', () => {
    toastForAlert({ ...base, ackedAt: '2026-09-28T12:00:00Z', ackedBy: 'bob' })
    toastForAlert({ ...base, state: 'resolved', resolvedAt: '2026-09-28T12:01:00Z' })
    expect(vi.mocked(toast).mock.calls.map((c) => c[0])).toMatchObject([
      { id: 'alert-7', tone: 'info' },
      { id: 'alert-7', tone: 'success', title: 'Resolved: High CPU on x' },
    ])
  })
})
