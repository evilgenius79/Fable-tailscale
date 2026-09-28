import { describe, expect, it } from 'vitest'
import { QueryClient } from '@tanstack/react-query'
import { applyAlert, applyEvent, applyTick } from './sse'
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

describe('applyAlert', () => {
  it('updates cached alerts and the device detail open list', () => {
    const { state, qc } = seed()
    const a = state.alerts[0]!
    qc.setQueryData<Alert[]>(queryKeys.alerts({ state: 'open', limit: 200 }), [a])
    qc.setQueryData<DeviceDetail>(queryKeys.device(a.deviceId!), { device: state.devices[0]!, recentEvents: [], openAlerts: [a] })
    const resolved: Alert = { ...a, state: 'resolved', resolvedAt: new Date().toISOString() }
    applyAlert(qc, resolved)
    expect(qc.getQueryData<Alert[]>(queryKeys.alerts({ state: 'open', limit: 200 }))![0]!.state).toBe('resolved')
    expect(qc.getQueryData<DeviceDetail>(queryKeys.device(a.deviceId!))!.openAlerts).toEqual([])
  })
})
