import { useMemo, type ReactNode } from 'react'
import type { UseQueryResult } from '@tanstack/react-query'
import { useRules } from '../../api/hooks'
import type { AlertRuleType, Device, Series } from '../../api/types'
import { formatBitrate, formatLatency, formatLoad, formatPercent, formatTemp } from '../../lib/format'
import type { RangeKey } from '../../lib/time'
import { TimeSeriesChart, useChartTheme, type TimeSeriesSeries } from '../charts'
import { Card, CardHeader } from '../ui/Card'
import { seriesHas, seriesRows, type SeriesKey } from './deviceDetail'

export const DEVICE_CHART_SYNC = 'device-detail'

interface ChartCardProps {
  title: string
  description?: string
  /** Current value shown top-right (text tokens, never series colour). */
  current?: ReactNode
  children: ReactNode
}

function ChartCard({ title, description, current, children }: ChartCardProps) {
  return (
    <Card className="min-w-0">
      <CardHeader
        title={title}
        description={description}
        className="mb-2"
        actions={current !== undefined && current !== null ? <span className="text-sm font-semibold num text-fg">{current}</span> : null}
      />
      {children}
    </Card>
  )
}

export interface DeviceChartsProps {
  device: Device
  range: RangeKey
  series: UseQueryResult<Series>
  /** Show the system-metric charts (CPU, memory, …). */
  withMetrics: boolean
}

/**
 * The chart grid for the Overview tab. One y-axis per chart; rollup ranges add
 * a dashed "max" envelope; alert-rule thresholds render as reference lines.
 */
export function DeviceCharts({ device, range, series, withMetrics }: DeviceChartsProps) {
  const theme = useChartTheme()
  const rules = useRules()
  const points = series.data?.points
  const rows = useMemo(() => seriesRows(points ?? []), [points])
  const rollup = series.data?.source === 'rollup'
  const has = (k: SeriesKey) => !!points && seriesHas(points, k)

  const threshold = (type: AlertRuleType): number | undefined => {
    const r = rules.data?.find((x) => x.type === type)
    return r && r.enabled && r.threshold > 0 ? r.threshold : undefined
  }
  const maxOf = (key: SeriesKey): number => {
    let max = 0
    for (const p of points ?? []) {
      const v = p[key]
      if (typeof v === 'number' && v > max) max = v
    }
    return max
  }
  /**
   * A reference line on an auto-scaled axis extends the domain; only draw it
   * when the data reaches at least a quarter of it, otherwise the line would
   * flatten the series into the baseline (the threshold stays in the rule UI).
   */
  const guide = (y: number | undefined, key: SeriesKey, label: string, tone?: 'critical'): Array<{ y: number; label: string; tone?: 'critical' }> =>
    y && maxOf(key) >= y * 0.25 ? [{ y, label, tone }] : []
  /** Axis labels must not wrap ("260 ms" → one line). */
  const axis =
    (f: (v: number) => string) =>
    (v: number): string =>
      f(v).replace(/ /g, ' ')

  const common = {
    range,
    height: 200,
    syncId: DEVICE_CHART_SYNC,
    loading: series.isPending,
    fetching: series.isFetching && !series.isPending,
    error: series.data ? undefined : series.error,
    onRetry: () => void series.refetch(),
  }
  const m = device.metrics
  const c = device.connectivity

  const envelope = (key: SeriesKey, label = 'Max'): TimeSeriesSeries[] =>
    rollup && has(key) ? [{ key, label, color: theme.series[0]!, dashed: true, kind: 'area' as const }] : []

  const latencyRef = threshold('high_latency')
  const cpuRef = threshold('high_cpu')
  const memRef = threshold('high_memory')
  const diskRef = threshold('disk_full')
  const tempRef = threshold('high_temperature')
  const loadRatio = threshold('high_load')

  return (
    <div className="grid gap-4 lg:grid-cols-2">
      <ChartCard title="Latency" description="Tailscale ping round-trip" current={device.online ? formatLatency(c.latencyMs) : undefined}>
        <TimeSeriesChart
          {...common}
          data={rows}
          series={[{ key: 'latencyMs', label: rollup ? 'Average' : 'Latency', color: theme.series[0]! }, ...envelope('latencyMax')]}
          yFormat={axis(formatLatency)}
          referenceLines={guide(latencyRef, 'latencyMs', 'alert threshold', 'critical')}
          ariaLabel={`Latency to ${device.name}`}
        />
      </ChartCard>
      <ChartCard title="Tailscale throughput" description="WireGuard traffic to and from this peer" current={device.online ? `↓ ${formatBitrate(c.rxRate)} · ↑ ${formatBitrate(c.txRate)}` : undefined}>
        <TimeSeriesChart
          {...common}
          data={rows}
          series={[
            { key: 'tsRxRate', label: 'Download' },
            { key: 'tsTxRate', label: 'Upload' },
          ]}
          yFormat={axis(formatBitrate)}
          ariaLabel={`Tailscale throughput for ${device.name}`}
        />
      </ChartCard>
      {withMetrics ? (
        <>
          <ChartCard title="CPU" description={m?.cpuModel ?? 'Utilisation across all cores'} current={m ? formatPercent(m.cpuPercent) : undefined}>
            <TimeSeriesChart
              {...common}
              data={rows}
              series={[{ key: 'cpu', label: rollup ? 'Average' : 'CPU', color: theme.series[0]! }, ...envelope('cpuMax')]}
              yDomain={[0, 100]}
              yFormat={formatPercent}
              referenceLines={cpuRef ? [{ y: cpuRef, label: 'alert threshold', tone: 'critical' }] : undefined}
              ariaLabel={`CPU usage on ${device.name}`}
            />
          </ChartCard>
          <ChartCard title="Memory" description="Used memory" current={m ? formatPercent(m.memPercent) : undefined}>
            <TimeSeriesChart
              {...common}
              data={rows}
              series={[{ key: 'mem', label: 'Memory' }]}
              yDomain={[0, 100]}
              yFormat={formatPercent}
              referenceLines={memRef ? [{ y: memRef, label: 'alert threshold', tone: 'critical' }] : undefined}
              ariaLabel={`Memory usage on ${device.name}`}
            />
          </ChartCard>
          <ChartCard title="Disk" description="Fullest filesystem" current={m ? formatPercent(m.diskPercent) : undefined}>
            <TimeSeriesChart
              {...common}
              data={rows}
              series={[{ key: 'disk', label: 'Disk' }]}
              yDomain={[0, 100]}
              yFormat={formatPercent}
              referenceLines={diskRef ? [{ y: diskRef, label: 'alert threshold', tone: 'critical' }] : undefined}
              ariaLabel={`Disk usage on ${device.name}`}
            />
          </ChartCard>
          <ChartCard title="Device network" description="All physical interfaces" current={m ? `↓ ${formatBitrate(m.netRxRate)} · ↑ ${formatBitrate(m.netTxRate)}` : undefined}>
            <TimeSeriesChart
              {...common}
              data={rows}
              series={[
                { key: 'netRxRate', label: 'Download' },
                { key: 'netTxRate', label: 'Upload' },
              ]}
              yFormat={axis(formatBitrate)}
              ariaLabel={`Network throughput on ${device.name}`}
            />
          </ChartCard>
          {has('tempC') || m?.temperatures?.length ? (
            <ChartCard title="Temperature" description={m?.temperatures?.[0]?.sensor ?? 'Hottest sensor'} current={m?.temperatures?.length ? formatTemp(Math.max(...m.temperatures.map((t) => t.celsius))) : undefined}>
              <TimeSeriesChart
                {...common}
                data={rows}
                series={[{ key: 'tempC', label: 'Temperature' }]}
                yFormat={formatTemp}
                referenceLines={guide(tempRef, 'tempC', 'alert threshold', 'critical')}
                ariaLabel={`Temperature on ${device.name}`}
              />
            </ChartCard>
          ) : null}
          {has('load1') || m ? (
            <ChartCard title="Load average" description={m ? `1 minute · ${m.cpuCount} cores` : '1 minute'} current={m ? formatLoad(m.load1) : undefined}>
              <TimeSeriesChart
                {...common}
                data={rows}
                series={[{ key: 'load1', label: 'Load (1m)' }]}
                yFormat={formatLoad}
                referenceLines={[...guide(m?.cpuCount, 'load1', `${m?.cpuCount ?? 0} cores`), ...guide(loadRatio && m?.cpuCount ? loadRatio * m.cpuCount : undefined, 'load1', 'alert threshold', 'critical')]}
                ariaLabel={`Load average on ${device.name}`}
              />
            </ChartCard>
          ) : null}
        </>
      ) : null}
    </div>
  )
}
