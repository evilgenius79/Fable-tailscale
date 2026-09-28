import { useMemo } from 'react'
import type { UseQueryResult } from '@tanstack/react-query'
import { Activity, ArrowDown, ArrowDownUp, ArrowUp, Cpu, Gauge, HardDrive, ListTree, MemoryStick, Power, Thermometer, Timer, Waypoints } from 'lucide-react'
import type { Device, Series, UptimeReport } from '../../api/types'
import { formatBitrate, formatBytes, formatDateTime, formatDuration, formatInt, formatLatency, formatLoad, formatPercent, formatRelative, formatTemp, formatUptimePct, plural } from '../../lib/format'
import { pathLabel, pathTone, utilizationTone, type StatusTone } from '../../lib/status'
import type { RangeKey } from '../../lib/time'
import { StatTile, type StatTileProps } from '../ui/StatTile'
import { AgentEmptyState } from './AgentEmptyState'
import { AvailabilityTimeline } from './AvailabilityTimeline'
import { DeviceCharts } from './DeviceCharts'
import { hasMetrics, loadPercent, seriesHas, trend } from './deviceDetail'

export interface OverviewTabProps {
  device: Device
  range: RangeKey
  series: UseQueryResult<Series>
  uptime: UseQueryResult<UptimeReport>
  agentPort: number
  agentEnabled: boolean
}

function uptimeTone(pct: number | undefined): StatusTone | 'default' {
  if (pct === undefined) return 'default'
  if (pct < 95) return 'critical'
  if (pct < 99) return 'warning'
  return 'online'
}

/** KPI tiles, availability timeline and the chart grid. */
export function OverviewTab({ device, range, series, uptime, agentPort, agentEnabled }: OverviewTabProps) {
  const points = series.data?.points
  const m = hasMetrics(device) ? device.metrics : undefined
  const c = device.connectivity
  const u = device.uptime
  const withMetrics = !!m || (!!points && seriesHas(points, 'cpu'))

  const tiles = useMemo<StatTileProps[]>(() => {
    const p = points ?? []
    const rx = (
      <span className="inline-flex items-center gap-1">
        <ArrowDown className="size-4 text-fg-muted" aria-hidden="true" />
        <span className="sr-only">Download </span>
        {formatBitrate(c.rxRate)}
      </span>
    )
    const out: StatTileProps[] = [
      {
        label: 'Uptime · 24h',
        value: formatUptimePct(u.pct24h),
        hint: `7d ${formatUptimePct(u.pct7d)} · 30d ${formatUptimePct(u.pct30d)}`,
        tone: uptimeTone(u.pct24h),
        icon: Activity,
      },
      {
        label: 'Latency',
        value: device.online ? formatLatency(c.latencyMs) : '—',
        hint: c.lastPing ? `pinged ${formatRelative(c.lastPing)}` : device.online ? 'no ping yet' : 'offline',
        trend: trend(p, 'latencyMs'),
        tone: !device.online || c.latencyMs === undefined ? 'default' : c.latencyMs >= 250 ? 'warning' : 'info',
        icon: Timer,
      },
      {
        label: 'Path',
        value: device.online ? pathLabel(c.path, c.relay) : 'Offline',
        hint: device.online ? (c.path === 'direct' ? c.curAddr ?? 'peer-to-peer' : c.path === 'relay' ? 'via DERP relay' : 'no path yet') : `last seen ${formatRelative(device.lastSeen)}`,
        tone: device.online ? pathTone(c.path) : 'offline',
        icon: Waypoints,
      },
      {
        label: 'Tailscale traffic',
        value: device.online ? rx : '—',
        hint: device.online ? (
          <span className="inline-flex items-center gap-1">
            <ArrowUp className="size-3 text-fg-muted" aria-hidden="true" />
            <span className="sr-only">Upload </span>
            {formatBitrate(c.txRate)}
          </span>
        ) : (
          'offline'
        ),
        trend: trend(p, 'tsRxRate'),
        icon: ArrowDownUp,
      },
    ]
    if (m) {
      const fullest = m.disks.reduce<(typeof m.disks)[number] | null>((a, d) => (a && a.percent >= d.percent ? a : d), null)
      const hottest = m.temperatures?.length ? m.temperatures.reduce((a, t) => (t.celsius > a.celsius ? t : a)) : null
      const loadPct = loadPercent(m.load1, m.cpuCount)
      out.push(
        { label: 'CPU', value: formatPercent(m.cpuPercent), hint: plural(m.cpuCount, 'core'), trend: trend(p, 'cpu'), tone: utilizationTone(m.cpuPercent), icon: Cpu },
        { label: 'Memory', value: formatPercent(m.memPercent), hint: `${formatBytes(m.memUsed)} of ${formatBytes(m.memTotal)}`, trend: trend(p, 'mem'), tone: utilizationTone(m.memPercent), icon: MemoryStick },
        {
          label: 'Disk',
          value: formatPercent(m.diskPercent),
          hint: fullest ? `${fullest.mount} · ${formatBytes(fullest.used)} of ${formatBytes(fullest.total)}` : undefined,
          trend: trend(p, 'disk'),
          tone: utilizationTone(m.diskPercent, 80, 95),
          icon: HardDrive,
        },
      )
      if (hottest) {
        out.push({
          label: 'Temperature',
          value: formatTemp(hottest.celsius),
          hint: hottest.sensor,
          trend: trend(p, 'tempC'),
          tone: hottest.celsius >= (hottest.critical ?? 85) ? 'critical' : hottest.celsius >= (hottest.critical ?? 85) - 10 ? 'warning' : 'default',
          icon: Thermometer,
        })
      }
      out.push(
        {
          label: 'Load average',
          value: formatLoad(m.load1),
          hint: `5m ${formatLoad(m.load5)} · 15m ${formatLoad(m.load15)}`,
          trend: trend(p, 'load1'),
          tone: loadPct === null ? 'default' : utilizationTone(loadPct, 100, 200),
          icon: Gauge,
        },
        { label: 'System uptime', value: formatDuration(m.uptimeSeconds), hint: m.bootTime ? `since ${formatDateTime(m.bootTime)}` : undefined, icon: Power },
        { label: 'Processes', value: formatInt(m.processes), hint: `sampled ${formatRelative(m.sampledAt)}`, icon: ListTree },
      )
    }
    return out
  }, [device.online, device.lastSeen, c, u, m, points])

  return (
    <div className="space-y-4 xl:space-y-6">
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 2xl:grid-cols-6" aria-label="Key metrics">
        {tiles.map((t, i) => (
          <StatTile key={i} size="sm" {...t} />
        ))}
      </div>

      <AvailabilityTimeline report={uptime.data} range={range} loading={uptime.isPending} fetching={uptime.isFetching && !uptime.isPending} error={uptime.error} onRetry={() => void uptime.refetch()} />

      <DeviceCharts device={device} range={range} series={series} withMetrics={withMetrics} />

      {!withMetrics ? <AgentEmptyState device={device} agentPort={agentPort} agentEnabled={agentEnabled} /> : null}
    </div>
  )
}
