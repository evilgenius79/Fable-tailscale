import { describe, expect, it } from 'vitest'
import {
  daysUntil,
  formatBitrate,
  formatByteRate,
  formatBytes,
  formatBytesSI,
  formatCompact,
  formatDuration,
  formatInt,
  formatLatency,
  formatMs,
  formatNumber,
  formatPercent,
  formatRatio,
  formatRelative,
  formatTemp,
  formatUptimePct,
  plural,
  shortDnsName,
  shortVersion,
  tagLabel,
  truncateMiddle,
} from './format'

describe('formatBytes', () => {
  it('formats IEC units', () => {
    expect(formatBytes(0)).toBe('0 B')
    expect(formatBytes(512)).toBe('512 B')
    expect(formatBytes(1024)).toBe('1 KiB')
    expect(formatBytes(1536)).toBe('1.5 KiB')
    expect(formatBytes(5 * 1024 ** 3)).toBe('5 GiB')
    expect(formatBytes(1.25 * 1024 ** 4)).toBe('1.3 TiB')
  })
  it('handles missing and negative', () => {
    expect(formatBytes(null)).toBe('—')
    expect(formatBytes(undefined)).toBe('—')
    expect(formatBytes(Number.NaN)).toBe('—')
    expect(formatBytes(-2048)).toBe('-2 KiB')
  })
  it('SI variant', () => {
    expect(formatBytesSI(1500)).toBe('1.5 kB')
    expect(formatBytesSI(2e12)).toBe('2 TB')
  })
})

describe('rates', () => {
  it('formatBitrate converts bytes/s to bits/s', () => {
    expect(formatBitrate(0)).toBe('0 b/s')
    expect(formatBitrate(125)).toBe('1 kb/s')
    expect(formatBitrate(125000)).toBe('1 Mb/s')
    expect(formatBitrate(1.5e8)).toBe('1.2 Gb/s')
    expect(formatBitrate(null)).toBe('—')
    expect(formatBitrate(-5)).toBe('0 b/s')
  })
  it('formatByteRate', () => {
    expect(formatByteRate(1536)).toBe('1.5 KiB/s')
    expect(formatByteRate(undefined)).toBe('—')
  })
})

describe('durations', () => {
  it('formatDuration picks the two largest units', () => {
    expect(formatDuration(0)).toBe('0s')
    expect(formatDuration(45)).toBe('45s')
    expect(formatDuration(61)).toBe('1m 1s')
    expect(formatDuration(3661)).toBe('1h 1m')
    expect(formatDuration(90000)).toBe('1d 1h')
    expect(formatDuration(90000, 3)).toBe('1d 1h')
    expect(formatDuration(400 * 86400)).toBe('1y 35d')
    expect(formatDuration(null)).toBe('—')
    expect(formatDuration(-5)).toBe('0s')
  })
  it('formatMs', () => {
    expect(formatMs(850)).toBe('850ms')
    expect(formatMs(2500)).toBe('2.5s')
    expect(formatMs(65000)).toBe('1m 5s')
  })
  it('formatLatency scales precision', () => {
    expect(formatLatency(0.42)).toBe('0.42 ms')
    expect(formatLatency(4.26)).toBe('4.3 ms')
    expect(formatLatency(12.6)).toBe('13 ms')
    expect(formatLatency(250)).toBe('250 ms')
    expect(formatLatency(1500)).toBe('1.5 s')
    expect(formatLatency(undefined)).toBe('—')
    expect(formatLatency(-1)).toBe('—')
  })
})

describe('percent', () => {
  it('formatPercent', () => {
    expect(formatPercent(87.456)).toBe('87%')
    expect(formatPercent(87.456, 1)).toBe('87.5%')
    expect(formatPercent(50, 2)).toBe('50%')
    expect(formatPercent(null)).toBe('—')
  })
  it('formatRatio', () => {
    expect(formatRatio(0.9987)).toBe('99.87%')
    expect(formatRatio(1)).toBe('100%')
  })
  it('formatUptimePct keeps precision near 100', () => {
    expect(formatUptimePct(100)).toBe('100%')
    expect(formatUptimePct(99.995)).toBe('99.995%')
    expect(formatUptimePct(99.9)).toBe('99.9%')
    expect(formatUptimePct(99.123)).toBe('99.12%')
    expect(formatUptimePct(90.14)).toBe('90.1%')
    expect(formatUptimePct(undefined)).toBe('—')
  })
})

describe('numbers', () => {
  it('formatCompact', () => {
    expect(formatCompact(999)).toBe('999')
    expect(formatCompact(1284)).toBe('1.3K')
    expect(formatCompact(12900)).toBe('13K')
    expect(formatCompact(4_200_000)).toBe('4.2M')
    expect(formatCompact(-1500)).toBe('-1.5K')
    expect(formatCompact(null)).toBe('—')
  })
  it('formatInt and formatNumber', () => {
    expect(formatInt(1234567)).toBe('1,234,567')
    expect(formatNumber(3.14159, 2)).toBe('3.14')
    expect(formatNumber(3, 2)).toBe('3')
  })
  it('formatTemp', () => {
    expect(formatTemp(71.6)).toBe('72°C')
  })
})

describe('relative time', () => {
  const now = new Date('2026-09-28T12:00:00Z')
  it('past', () => {
    expect(formatRelative('2026-09-28T11:59:58Z', now)).toBe('just now')
    expect(formatRelative('2026-09-28T11:59:18Z', now)).toBe('42s ago')
    expect(formatRelative('2026-09-28T11:55:00Z', now)).toBe('5m ago')
    expect(formatRelative('2026-09-28T09:00:00Z', now)).toBe('3h ago')
    expect(formatRelative('2026-09-26T12:00:00Z', now)).toBe('2d ago')
    expect(formatRelative('2026-07-01T12:00:00Z', now)).toBe('2mo ago')
    expect(formatRelative('2024-07-01T12:00:00Z', now)).toBe('2y ago')
  })
  it('future and missing', () => {
    expect(formatRelative('2026-10-01T12:00:00Z', now)).toBe('in 3d')
    expect(formatRelative(undefined, now)).toBe('—')
    expect(formatRelative('not a date', now)).toBe('—')
  })
  it('accepts unix seconds and ms', () => {
    const sec = Math.floor(now.getTime() / 1000) - 120
    expect(formatRelative(sec, now)).toBe('2m ago')
    expect(formatRelative(now.getTime() - 120_000, now)).toBe('2m ago')
  })
  it('daysUntil', () => {
    expect(daysUntil('2026-10-01T12:00:00Z', now)).toBe(3)
    expect(daysUntil(null, now)).toBeNull()
  })
})

describe('strings', () => {
  it('plural', () => {
    expect(plural(1, 'device')).toBe('1 device')
    expect(plural(3, 'device')).toBe('3 devices')
    expect(plural(0, 'entry', 'entries')).toBe('0 entries')
  })
  it('truncateMiddle', () => {
    expect(truncateMiddle('short')).toBe('short')
    expect(truncateMiddle('abcdefghijklmnopqrstuvwxyz', 11)).toBe('abcde…vwxyz')
  })
  it('shortDnsName', () => {
    expect(shortDnsName('nas.tail1234.ts.net')).toBe('nas')
    expect(shortDnsName('nas.tail1234.ts.net.')).toBe('nas')
    expect(shortDnsName('')).toBe('—')
  })
  it('tagLabel and shortVersion', () => {
    expect(tagLabel('tag:server')).toBe('server')
    expect(tagLabel('plain')).toBe('plain')
    expect(shortVersion('1.72.1-t1234abcd')).toBe('1.72.1')
    expect(shortVersion('v1.80')).toBe('1.80')
    expect(shortVersion(undefined)).toBe('—')
  })
})
