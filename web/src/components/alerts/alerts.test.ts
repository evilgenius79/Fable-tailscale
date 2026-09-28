import { describe, expect, it } from 'vitest'
import type { Alert, AlertRule } from '../../api/types'
import {
  RULE_TYPES,
  RULE_TYPE_ORDER,
  alertDeviceOptions,
  alertDurationSeconds,
  alertFiltersFromSearch,
  dayKey,
  dayLabel,
  draftFromRule,
  filterAlerts,
  formatAlertValue,
  formatForSeconds,
  formatThreshold,
  groupOpenAlerts,
  groupResolvedAlerts,
  joinSeconds,
  normalizeTag,
  ruleFromDraft,
  ruleMeta,
  ruleScopeSummary,
  splitSeconds,
  validateRuleDraft,
  validateTag,
} from './alerts'

const rule = (over: Partial<AlertRule> = {}): AlertRule => ({
  id: 'high_cpu',
  type: 'high_cpu',
  name: 'High CPU',
  enabled: true,
  severity: 'warning',
  threshold: 90,
  forSeconds: 600,
  notify: true,
  updatedAt: '2026-09-01T00:00:00Z',
  ...over,
})

const alert = (over: Partial<Alert> = {}): Alert => ({
  id: 1,
  ruleId: 'high_cpu',
  ruleType: 'high_cpu',
  deviceId: 'd1',
  deviceName: 'nas',
  state: 'open',
  severity: 'warning',
  title: 'High CPU on nas',
  message: 'CPU above 90%',
  openedAt: '2026-09-28T10:00:00Z',
  updatedAt: '2026-09-28T10:00:00Z',
  ...over,
})

describe('rule metadata', () => {
  it('covers every rule type exactly once in display order', () => {
    expect(new Set(RULE_TYPE_ORDER).size).toBe(RULE_TYPE_ORDER.length)
    expect(RULE_TYPE_ORDER.length).toBe(Object.keys(RULE_TYPES).length)
    for (const t of RULE_TYPE_ORDER) expect(RULE_TYPES[t].type).toBe(t)
  })

  it('matches the documented defaults', () => {
    expect(ruleMeta('device_offline').defaults).toEqual({ threshold: 0, forSeconds: 300, severity: 'warning' })
    expect(ruleMeta('high_latency').defaults).toEqual({ threshold: 250, forSeconds: 300, severity: 'info' })
    expect(ruleMeta('key_expiring').defaults).toEqual({ threshold: 7, forSeconds: 0, severity: 'warning' })
    expect(ruleMeta('high_load').defaults).toEqual({ threshold: 2, forSeconds: 600, severity: 'info' })
    expect(ruleMeta('new_device').defaults.forSeconds).toBe(86400)
  })

  it('maps units per type', () => {
    expect(ruleMeta('high_cpu').unit).toBe('percent')
    expect(ruleMeta('high_latency').unit).toBe('ms')
    expect(ruleMeta('high_temperature').unit).toBe('celsius')
    expect(ruleMeta('key_expiring').unit).toBe('days')
    expect(ruleMeta('high_load').unit).toBe('ratio')
    expect(ruleMeta('relay_only').unit).toBe('none')
    expect(ruleMeta('update_available').forLabel).toBeNull()
  })
})

describe('formatThreshold / formatForSeconds', () => {
  it('formats with the unit of the rule type', () => {
    expect(formatThreshold({ type: 'high_cpu', threshold: 90 })).toBe('90%')
    expect(formatThreshold({ type: 'high_latency', threshold: 250 })).toBe('250 ms')
    expect(formatThreshold({ type: 'high_temperature', threshold: 85 })).toBe('85 °C')
    expect(formatThreshold({ type: 'key_expiring', threshold: 7 })).toBe('7 days')
    expect(formatThreshold({ type: 'key_expiring', threshold: 1 })).toBe('1 day')
    expect(formatThreshold({ type: 'high_load', threshold: 2 })).toBe('2.0×')
    expect(formatThreshold({ type: 'device_offline', threshold: 0 })).toBe('—')
    expect(formatThreshold({ type: 'disk_full', threshold: 92.5 })).toBe('92.5%')
  })

  it('formats durations', () => {
    expect(formatForSeconds(0)).toBe('immediately')
    expect(formatForSeconds(undefined)).toBe('immediately')
    expect(formatForSeconds(300)).toBe('5m')
    expect(formatForSeconds(5400)).toBe('1h 30m')
    expect(formatForSeconds(86400)).toBe('1d')
  })

  it('splits and joins seconds', () => {
    expect(splitSeconds(600)).toEqual({ minutes: 10, seconds: 0 })
    expect(splitSeconds(95)).toEqual({ minutes: 1, seconds: 35 })
    expect(splitSeconds(-5)).toEqual({ minutes: 0, seconds: 0 })
    expect(joinSeconds(1, 35)).toBe(95)
    expect(joinSeconds(Number.NaN, 30)).toBe(30)
    expect(joinSeconds(2.9, 0)).toBe(120)
  })
})

describe('ruleScopeSummary', () => {
  it('describes scope', () => {
    expect(ruleScopeSummary({})).toBe('All devices')
    expect(ruleScopeSummary({ includeTags: ['tag:server'] })).toBe('Only tag:server')
    expect(ruleScopeSummary({ excludeDevices: ['a', 'b', 'c'] }, (id) => ({ a: 'alpha', b: 'beta' })[id])).toBe('Except alpha, beta +1')
    expect(ruleScopeSummary({ includeTags: ['tag:nas'], excludeTags: ['tag:lab'] })).toBe('Only tag:nas · Except tag:lab')
  })
})

describe('drafts', () => {
  it('round-trips a rule through a draft', () => {
    const r = rule({ includeTags: ['tag:server'], excludeDevices: ['x'], description: 'desc', forSeconds: 95 })
    const d = draftFromRule(r)
    expect(d).toMatchObject({ threshold: '90', minutes: '1', seconds: '35', includeTags: ['tag:server'], excludeDevices: ['x'] })
    expect(validateRuleDraft(d, r.type)).toEqual({})
    const back = ruleFromDraft(r, d)
    expect(back).toMatchObject({ threshold: 90, forSeconds: 95, name: 'High CPU', includeTags: ['tag:server'], excludeDevices: ['x'], description: 'desc' })
  })

  it('validates threshold ranges per unit', () => {
    const d = draftFromRule(rule())
    expect(validateRuleDraft({ ...d, threshold: '' }, 'high_cpu').threshold).toBe('Enter a number')
    expect(validateRuleDraft({ ...d, threshold: '101' }, 'high_cpu').threshold).toMatch(/at most 100/)
    expect(validateRuleDraft({ ...d, threshold: '0' }, 'high_cpu').threshold).toMatch(/at least 1/)
    expect(validateRuleDraft({ ...d, threshold: 'abc' }, 'device_offline').threshold).toBeUndefined()
  })

  it('validates duration inputs', () => {
    const d = draftFromRule(rule())
    expect(validateRuleDraft({ ...d, seconds: '60' }, 'high_cpu').duration).toMatch(/between 0 and 59/)
    expect(validateRuleDraft({ ...d, minutes: '1.5' }, 'high_cpu').duration).toMatch(/whole number/)
    expect(validateRuleDraft({ ...d, minutes: '-1' }, 'high_cpu').duration).toMatch(/whole number/)
    expect(validateRuleDraft({ ...d, minutes: '100000' }, 'high_cpu').duration).toMatch(/30 days/)
    // rules without a duration ignore the fields
    expect(validateRuleDraft({ ...d, minutes: 'x' }, 'key_expiring').duration).toBeUndefined()
  })

  it('rejects overlapping include/exclude scope and blank names', () => {
    const d = draftFromRule(rule())
    expect(validateRuleDraft({ ...d, name: '  ' }, 'high_cpu').name).toBe('Enter a name')
    expect(validateRuleDraft({ ...d, includeTags: ['tag:a'], excludeTags: ['tag:a'] }, 'high_cpu').scope).toMatch(/tag:a/)
    expect(validateRuleDraft({ ...d, includeDevices: ['x'], excludeDevices: ['x'] }, 'high_cpu').scope).toMatch(/device/)
  })

  it('zeroes threshold/duration for types that do not use them and dedupes scope', () => {
    const r = rule({ id: 'update_available', type: 'update_available', threshold: 0, forSeconds: 0 })
    const d = { ...draftFromRule(r), threshold: '55', minutes: '9', seconds: '9', includeTags: ['tag:a', 'tag:a', ' '] }
    const out = ruleFromDraft(r, d)
    expect(out.threshold).toBe(0)
    expect(out.forSeconds).toBe(0)
    expect(out.includeTags).toEqual(['tag:a'])
  })
})

describe('tags', () => {
  it('normalises and validates', () => {
    expect(normalizeTag(' Server ')).toBe('tag:server')
    expect(normalizeTag('tag:nas')).toBe('tag:nas')
    expect(normalizeTag('')).toBe('')
    expect(validateTag('tag:ok-1')).toBeNull()
    expect(validateTag('tag:Bad Tag')).toMatch(/tag:name/)
    expect(validateTag('tag:ok', ['tag:ok'])).toBe('Already in the list')
    expect(validateTag('')).toBe('Enter a tag name')
  })
})

describe('alert lists', () => {
  const alerts: Alert[] = [
    alert({ id: 1, severity: 'warning', openedAt: '2026-09-28T10:00:00Z' }),
    alert({ id: 2, severity: 'critical', openedAt: '2026-09-28T09:00:00Z', deviceId: 'd2', deviceName: 'pi', title: 'Disk full on pi', ruleType: 'disk_full' }),
    alert({ id: 3, severity: 'warning', openedAt: '2026-09-28T11:00:00Z', ackedAt: '2026-09-28T11:30:00Z', ackedBy: 'alice@example.com' }),
    alert({ id: 4, severity: 'info', openedAt: '2026-09-27T11:00:00Z', ruleType: 'update_available' }),
  ]

  it('filters by severity, device and search', () => {
    expect(filterAlerts(alerts, { severity: 'critical', device: '', q: '' }).map((a) => a.id)).toEqual([2])
    expect(filterAlerts(alerts, { severity: 'all', device: 'd2', q: '' }).map((a) => a.id)).toEqual([2])
    expect(filterAlerts(alerts, { severity: 'all', device: '', q: 'ALICE' }).map((a) => a.id)).toEqual([3])
    expect(filterAlerts(alerts, { severity: 'all', device: '', q: 'disk' }).map((a) => a.id)).toEqual([2])
  })

  it('groups open alerts by severity, unacked first then newest', () => {
    const groups = groupOpenAlerts(alerts)
    expect(groups.map((g) => g.key)).toEqual(['critical', 'warning', 'info'])
    expect(groups[1]!.alerts.map((a) => a.id)).toEqual([1, 3])
    expect(groups[0]!.tone).toBe('critical')
  })

  it('groups resolved alerts by day', () => {
    const now = new Date('2026-09-28T15:00:00')
    const resolved = [
      alert({ id: 10, state: 'resolved', resolvedAt: '2026-09-28T12:00:00', openedAt: '2026-09-28T11:00:00' }),
      alert({ id: 11, state: 'resolved', resolvedAt: '2026-09-27T12:00:00', openedAt: '2026-09-27T11:00:00' }),
      alert({ id: 12, state: 'resolved', resolvedAt: '2026-09-28T09:00:00', openedAt: '2026-09-28T08:00:00' }),
    ]
    const groups = groupResolvedAlerts(resolved, now)
    expect(groups.map((g) => g.label)).toEqual(['Today', 'Yesterday'])
    expect(groups[0]!.alerts.map((a) => a.id)).toEqual([10, 12])
  })

  it('lists device options sorted by name', () => {
    expect(alertDeviceOptions(alerts)).toEqual([
      { value: 'd1', label: 'nas' },
      { value: 'd2', label: 'pi' },
    ])
  })

  it('computes durations and values', () => {
    expect(alertDurationSeconds({ openedAt: '2026-09-28T10:00:00Z', resolvedAt: '2026-09-28T10:05:00Z' })).toBe(300)
    expect(alertDurationSeconds({ openedAt: '2026-09-28T10:00:00Z' }, Date.parse('2026-09-28T10:01:00Z'))).toBe(60)
    expect(formatAlertValue({ ruleType: 'high_cpu', value: 92.34 })).toBe('92.3%')
    expect(formatAlertValue({ ruleType: 'high_latency', value: 312.6 })).toBe('313 ms')
    expect(formatAlertValue({ ruleType: 'key_expiring', value: 3 })).toBe('3 days')
    expect(formatAlertValue({ ruleType: 'high_load', value: 2.5 })).toBe('2.50×')
    expect(formatAlertValue({ ruleType: 'device_offline', value: 5 })).toBeNull()
    expect(formatAlertValue({ ruleType: 'high_cpu' })).toBeNull()
  })
})

describe('alertFiltersFromSearch', () => {
  it('seeds severity and device from a deep link and ignores junk', () => {
    expect(alertFiltersFromSearch(new URLSearchParams('severity=critical&device=n1'))).toEqual({ severity: 'critical', device: 'n1', q: '' })
    expect(alertFiltersFromSearch(new URLSearchParams('severity=bogus'))).toEqual({ severity: 'all', device: '', q: '' })
  })
  it('leaves the user filter alone when the params are absent', () => {
    const mine = { severity: 'all' as const, device: 'n2', q: 'disk' }
    expect(alertFiltersFromSearch(new URLSearchParams('tab=resolved'), mine)).toEqual(mine)
  })
})

describe('day helpers', () => {
  it('keys and labels days in local time', () => {
    const now = new Date('2026-09-28T15:00:00')
    expect(dayKey(new Date('2026-09-28T00:30:00'))).toBe('2026-09-28')
    expect(dayLabel(new Date('2026-09-28T01:00:00'), now)).toBe('Today')
    expect(dayLabel(new Date('2026-09-27T23:00:00'), now)).toBe('Yesterday')
    expect(dayLabel(new Date('2026-09-22T12:00:00'), now)).toMatch(/^Tue, Sep 22$/)
    expect(dayLabel(new Date('2025-12-31T12:00:00'), now)).toBe('Dec 31, 2025')
  })
})
