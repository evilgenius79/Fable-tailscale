import { describe, expect, it } from 'vitest'
import { timeTicks } from './TimeSeriesChart'
import { sparklinePaths } from '../ui/Sparkline'
import { readChartTheme, seriesColor } from './ChartTheme'

describe('timeTicks', () => {
  it('produces boundary-aligned ticks within the domain', () => {
    const min = 1_700_000_000
    const max = min + 3600
    const ticks = timeTicks(min, max, 6)
    expect(ticks.length).toBeGreaterThan(2)
    expect(ticks.length).toBeLessThanOrEqual(7)
    expect(ticks.every((t) => t >= min && t <= max)).toBe(true)
    const step = ticks[1]! - ticks[0]!
    expect(ticks.every((t, i) => i === 0 || t - ticks[i - 1]! === step)).toBe(true)
  })
  it('handles degenerate input', () => {
    expect(timeTicks(10, 10)).toEqual([])
    expect(timeTicks(10, 5)).toEqual([])
  })
})

describe('sparklinePaths', () => {
  it('draws a single segment for continuous data', () => {
    const g = sparklinePaths([1, 2, 3], 100, 30)
    expect(g.line.startsWith('M')).toBe(true)
    expect(g.line.split('M').length - 1).toBe(1)
    expect(g.area.endsWith('Z')).toBe(true)
    expect(g.last).not.toBeNull()
  })
  it('breaks segments at nulls', () => {
    const g = sparklinePaths([1, 2, null, 3, 4], 100, 30)
    expect(g.line.split('M').length - 1).toBe(2)
    expect(g.area.split('Z').length - 1).toBe(2)
  })
  it('is empty without values', () => {
    expect(sparklinePaths([null, undefined], 100, 30).line).toBe('')
    expect(sparklinePaths([1, 2], 0, 30).line).toBe('')
  })
})

describe('chart theme', () => {
  it('falls back to 8 validated slots and never cycles', () => {
    const t = readChartTheme('dark')
    expect(t.series.length).toBe(8)
    expect(seriesColor(t, 0)).toBe(t.series[0])
    expect(seriesColor(t, 9)).toBe(t.muted)
  })
})
