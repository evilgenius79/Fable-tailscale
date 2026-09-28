import { describe, expect, it } from 'vitest'
import { PROFILES, buildDevice } from '../../api/mockData'
import type { Device } from '../../api/types'
import {
  DEFAULT_FILTERS,
  DEFAULT_SORT,
  SORT_KEYS,
  applyFilters,
  countActiveFilters,
  filtersEqual,
  hasAnyFilter,
  hasFlag,
  ipSortKey,
  isKeyExpiring,
  isSubnetRouter,
  keyExpiryDays,
  matchesSearch,
  osFacets,
  parseFilters,
  parseSort,
  serializeView,
  sortDevices,
  statusRank,
  tagFacets,
  totalRates,
  userFacets,
} from './deviceFilters'

// Fix "now" to a weekday afternoon so the deterministic fixtures are stable.
const NOW = Date.UTC(2026, 8, 28, 15, 0, 0)
const devices: Device[] = PROFILES.map((p) => buildDevice(p, NOW))
const byName = (name: string): Device => {
  const d = devices.find((x) => x.name === name)
  if (!d) throw new Error(`fixture ${name} missing`)
  return d
}

describe('flags', () => {
  it('detects subnet routers (non-default enabled routes only)', () => {
    expect(isSubnetRouter(byName('office-router'))).toBe(true)
    expect(isSubnetRouter(byName('nas-basement'))).toBe(true)
    // exit node advertises only default routes
    expect(isSubnetRouter(byName('edge-exit-1'))).toBe(false)
    expect(isSubnetRouter(byName('alice-mbp'))).toBe(false)
  })
  it('computes key expiry days and the expiring flag', () => {
    const media = byName('media-box')
    expect(keyExpiryDays(media, NOW)).toBeCloseTo(3, 5)
    expect(isKeyExpiring(media, NOW)).toBe(true)
    expect(isKeyExpiring(byName('alice-mbp'), NOW)).toBe(false)
    const disabled: Device = { ...media, keyExpiryDisabled: true }
    expect(keyExpiryDays(disabled, NOW)).toBeNull()
    expect(isKeyExpiring(disabled, NOW)).toBe(false)
    const expired: Device = { ...media, expired: true }
    expect(isKeyExpiring(expired, NOW)).toBe(false)
  })
  it('hasFlag covers every flag', () => {
    expect(hasFlag(byName('alice-mbp'), 'update', NOW)).toBe(true)
    expect(hasFlag(byName('new-laptop-7f2a'), 'unauthorized', NOW)).toBe(true)
    expect(hasFlag(byName('edge-exit-1'), 'exitNode', NOW)).toBe(true)
    expect(hasFlag(byName('office-router'), 'subnetRouter', NOW)).toBe(true)
    expect(hasFlag(byName('netwatch-hub'), 'agent', NOW)).toBe(true)
    expect(hasFlag(byName('bob-thinkpad'), 'agent', NOW)).toBe(false)
    expect(hasFlag(byName('media-box'), 'keyExpiring', NOW)).toBe(true)
  })
})

describe('matchesSearch', () => {
  const nas = byName('nas-basement')
  it('matches name, ip, user, tag, model and os, case-insensitively', () => {
    expect(matchesSearch(nas, 'NAS')).toBe(true)
    expect(matchesSearch(nas, '100.64.0.2')).toBe(true)
    expect(matchesSearch(nas, 'alice')).toBe(true)
    expect(matchesSearch(nas, 'tag:nas')).toBe(true)
    expect(matchesSearch(nas, 'synology')).toBe(true)
    expect(matchesSearch(nas, 'linux')).toBe(true)
    expect(matchesSearch(nas, 'pixel')).toBe(false)
  })
  it('requires every token to match', () => {
    expect(matchesSearch(nas, 'alice synology')).toBe(true)
    expect(matchesSearch(nas, 'alice windows')).toBe(false)
    expect(matchesSearch(nas, '   ')).toBe(true)
  })
})

describe('applyFilters', () => {
  it('returns everything for the defaults', () => {
    expect(applyFilters(devices, DEFAULT_FILTERS, NOW)).toHaveLength(devices.length)
  })
  it('filters by status and path', () => {
    const online = applyFilters(devices, { ...DEFAULT_FILTERS, status: 'online' }, NOW)
    expect(online.every((d) => d.online)).toBe(true)
    const offline = applyFilters(devices, { ...DEFAULT_FILTERS, status: 'offline' }, NOW)
    expect(offline.every((d) => !d.online)).toBe(true)
    expect(online.length + offline.length).toBe(devices.length)
    const relay = applyFilters(devices, { ...DEFAULT_FILTERS, path: 'relay' }, NOW)
    expect(relay.length).toBeGreaterThan(0)
    expect(relay.every((d) => d.connectivity.path === 'relay')).toBe(true)
  })
  it('filters by os family, user, tag and flags', () => {
    const mac = applyFilters(devices, { ...DEFAULT_FILTERS, os: ['macos'] }, NOW)
    expect(mac.map((d) => d.name).sort()).toEqual(['alice-mbp', 'new-laptop-7f2a'])
    const phones = applyFilters(devices, { ...DEFAULT_FILTERS, os: ['ios', 'android'] }, NOW)
    expect(phones.map((d) => d.name).sort()).toEqual(['alice-iphone', 'bob-pixel'])
    const bob = applyFilters(devices, { ...DEFAULT_FILTERS, user: 'bob@example.com' }, NOW)
    expect(bob.every((d) => d.user === 'bob@example.com')).toBe(true)
    expect(bob.length).toBe(3)
    const servers = applyFilters(devices, { ...DEFAULT_FILTERS, tag: 'tag:server' }, NOW)
    expect(servers.every((d) => d.tags.includes('tag:server'))).toBe(true)
    expect(servers.length).toBe(4)
    const updates = applyFilters(devices, { ...DEFAULT_FILTERS, flags: ['update'] }, NOW)
    expect(updates.map((d) => d.name).sort()).toEqual(['alice-mbp', 'backup-server'])
    const both = applyFilters(devices, { ...DEFAULT_FILTERS, flags: ['update', 'agent'] }, NOW)
    expect(both.map((d) => d.name)).toEqual(['alice-mbp', 'backup-server'].filter((n) => byName(n).agent.state === 'reachable'))
  })
  it('combines search with filters', () => {
    const r = applyFilters(devices, { ...DEFAULT_FILTERS, q: 'server', status: 'online' }, NOW)
    expect(r.length).toBeGreaterThan(0)
    expect(r.every((d) => d.online)).toBe(true)
  })
})

describe('active filter accounting', () => {
  it('counts each active dimension', () => {
    expect(countActiveFilters(DEFAULT_FILTERS)).toBe(0)
    expect(countActiveFilters({ ...DEFAULT_FILTERS, status: 'online', os: ['linux', 'macos'], flags: ['update'], user: 'a', tag: 't', path: 'relay' })).toBe(7)
    expect(hasAnyFilter({ ...DEFAULT_FILTERS, q: 'x' })).toBe(true)
    expect(hasAnyFilter(DEFAULT_FILTERS)).toBe(false)
  })
})

describe('URL round trip', () => {
  it('omits defaults and restores every field', () => {
    const f = { ...DEFAULT_FILTERS, q: 'nas', status: 'online' as const, path: 'direct' as const, os: ['linux' as const, 'ios' as const], user: 'alice@example.com', tag: 'tag:nas', flags: ['update' as const, 'agent' as const] }
    const sp = serializeView(f, { key: 'latency', dir: 'desc' })
    expect(sp.get('os')).toBe('linux,ios')
    expect(sp.get('flags')).toBe('update,agent')
    expect(sp.get('sort')).toBe('latency:desc')
    expect(parseFilters(sp)).toEqual(f)
    expect(parseSort(sp, SORT_KEYS)).toEqual({ key: 'latency', dir: 'desc' })
    expect(serializeView(DEFAULT_FILTERS, DEFAULT_SORT).toString()).toBe('')
    expect(filtersEqual(parseFilters(sp), f)).toBe(true)
  })
  it('ignores unknown or malformed values', () => {
    const sp = new URLSearchParams('status=maybe&path=teleport&os=beos,linux&flags=bogus,update&sort=nope:desc')
    const f = parseFilters(sp)
    expect(f.status).toBe('all')
    expect(f.path).toBe('all')
    expect(f.os).toEqual(['linux'])
    expect(f.flags).toEqual(['update'])
    expect(parseSort(sp, SORT_KEYS)).toEqual(DEFAULT_SORT)
    expect(parseSort(new URLSearchParams('sort=name:sideways'), SORT_KEYS)).toEqual({ key: 'name', dir: 'asc' })
  })
  it('accepts repeated params as well as comma lists', () => {
    const f = parseFilters(new URLSearchParams('os=linux&os=macos&os=linux'))
    expect(f.os).toEqual(['linux', 'macos'])
  })
})

describe('sorting', () => {
  it('sorts by name ascending by default', () => {
    const names = sortDevices(devices, null).map((d) => d.name)
    expect(names).toEqual([...names].sort((a, b) => a.localeCompare(b, undefined, { numeric: true, sensitivity: 'base' })))
  })
  it('puts devices without a value last in either direction', () => {
    const asc = sortDevices(devices, { key: 'latency', dir: 'asc' })
    const desc = sortDevices(devices, { key: 'latency', dir: 'desc' })
    const offlineCount = devices.filter((d) => !d.online).length
    expect(asc.slice(-offlineCount).every((d) => !d.online)).toBe(true)
    expect(desc.slice(-offlineCount).every((d) => !d.online)).toBe(true)
    expect(asc[0]!.connectivity.latencyMs).toBeLessThanOrEqual(asc[1]!.connectivity.latencyMs!)
    expect(desc[0]!.connectivity.latencyMs).toBeGreaterThanOrEqual(desc[1]!.connectivity.latencyMs!)
  })
  it('ranks status attention-first', () => {
    expect(statusRank(byName('new-laptop-7f2a'))).toBe(0)
    expect(statusRank(byName('alice-iphone'))).toBe(3)
    expect(statusRank(byName('netwatch-hub'))).toBe(2)
    const sorted = sortDevices(devices, { key: 'status', dir: 'asc' })
    expect(sorted[0]!.name).toBe('new-laptop-7f2a')
    expect(sorted[sorted.length - 1]!.online).toBe(false)
  })
  it('orders IPv4 numerically', () => {
    expect(ipSortKey('100.64.0.2')! < ipSortKey('100.64.0.10')!).toBe(true)
    expect(ipSortKey('fd7a::1')).toBe('~fd7a::1')
    expect(ipSortKey(undefined)).toBeNull()
    const ips = sortDevices(devices, { key: 'ip', dir: 'asc' }).map((d) => d.addresses[0])
    expect(ips[0]).toBe('100.64.0.1')
    expect(ips[ips.length - 1]).toBe('100.64.0.17')
  })
  it('exposes every column sort key', () => {
    for (const k of ['name', 'status', 'os', 'ip', 'user', 'path', 'latency', 'rx', 'tx', 'throughput', 'cpu', 'mem', 'disk', 'uptime', 'version', 'keyExpiry', 'lastSeen']) {
      expect(SORT_KEYS).toContain(k)
      expect(() => sortDevices(devices, { key: k, dir: 'desc' })).not.toThrow()
    }
  })
})

describe('facets', () => {
  it('summarises os families in canonical order', () => {
    const f = osFacets(devices)
    expect(f.map((x) => x.value)).toEqual(['linux', 'macos', 'windows', 'ios', 'android'])
    expect(f.reduce((a, b) => a + b.count, 0)).toBe(devices.length)
  })
  it('summarises users by count then label', () => {
    const f = userFacets(devices)
    expect(f[0]!.value).toBe('alice@example.com')
    expect(f[0]!.label).toBe('Alice Park')
    expect(f.find((x) => x.value === 'tagged-devices')?.label).toBe('tagged-devices')
  })
  it('summarises tags', () => {
    const f = tagFacets(devices)
    expect(f[0]).toEqual({ value: 'tag:server', label: 'tag:server', count: 4 })
  })
  it('totals live rates over online devices', () => {
    const t = totalRates(devices)
    expect(t.online).toBe(devices.filter((d) => d.online).length)
    expect(t.rx).toBeGreaterThan(0)
    expect(t.tx).toBeGreaterThan(0)
  })
})
