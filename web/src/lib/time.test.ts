import { describe, expect, it } from 'vitest'
import { RANGE_PRESETS, formatTick, isRangeKey, rangePreset, rangeRefetchMs, rangeSeconds, rangeShowsDate, rangeStepSeconds, rangeWindow, tickCountForWidth } from './time'

describe('range presets', () => {
  it('has all documented ranges in order', () => {
    expect(RANGE_PRESETS.map((p) => p.key)).toEqual(['15m', '1h', '3h', '6h', '12h', '24h', '2d', '7d', '14d', '30d'])
  })
  it('isRangeKey guards', () => {
    expect(isRangeKey('24h')).toBe(true)
    expect(isRangeKey('90d')).toBe(false)
    expect(isRangeKey(24)).toBe(false)
  })
  it('rangeSeconds and preset lookup', () => {
    expect(rangeSeconds('1h')).toBe(3600)
    expect(rangeSeconds('7d')).toBe(7 * 86400)
    expect(rangePreset('30d').longLabel).toBe('Last 30 days')
  })
  it('matches the docs/API.md step table', () => {
    expect(rangeStepSeconds('15m')).toBe(15)
    expect(rangeStepSeconds('3h')).toBe(15)
    expect(rangeStepSeconds('24h')).toBe(60)
    expect(rangeStepSeconds('2d')).toBe(300)
    expect(rangeStepSeconds('7d')).toBe(300)
    expect(rangeStepSeconds('14d')).toBe(1800)
    expect(rangeStepSeconds('30d')).toBe(3600)
  })
  it('refetch cadence grows with range', () => {
    expect(rangeRefetchMs('1h')).toBeLessThan(rangeRefetchMs('24h'))
    expect(rangeRefetchMs('24h')).toBeLessThan(rangeRefetchMs('7d'))
  })
  it('rangeShowsDate', () => {
    expect(rangeShowsDate('24h')).toBe(false)
    expect(rangeShowsDate('2d')).toBe(true)
  })
  it('rangeWindow', () => {
    const w = rangeWindow('1h', 1_700_000_000_000)
    expect(w.to - w.from).toBe(3600)
    expect(w.to).toBe(1_700_000_000)
  })
})

describe('ticks', () => {
  it('formatTick uses time inside a day and dates beyond a week', () => {
    const t = Math.floor(new Date('2026-09-28T14:05:00').getTime() / 1000)
    expect(formatTick(t, '1h')).toMatch(/^\d{2}:\d{2}$/)
    expect(formatTick(t, '7d')).toMatch(/^[A-Z][a-z]{2} \d{2}$/)
    expect(formatTick(t, '30d')).toBe('Sep 28')
  })
  it('tickCountForWidth', () => {
    expect(tickCountForWidth(300)).toBe(3)
    expect(tickCountForWidth(1400)).toBe(8)
  })
})
