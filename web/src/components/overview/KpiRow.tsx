import { Activity, Bell, Download, KeyRound, MonitorSmartphone, Radio, ShieldAlert, Timer, Waypoints } from 'lucide-react'
import type { Overview } from '../../api/types'
import { cn } from '../../lib/cn'
import { formatBitrate, formatInt, formatLatency, formatPercent, plural } from '../../lib/format'
import { StatTile } from '../ui/StatTile'
import { deltaVsStart, percentOf, sparklineWindow, sumSeries, windowLabel } from './overview'

export interface KpiRowProps {
  overview: Overview | undefined
  loading: boolean
}

/** Two rows of KPI tiles: fleet health (with sparklines) and attention counters. */
export function KpiRow({ overview: o, loading }: KpiRowProps) {
  const s = o?.sparklines
  const total = s ? sumSeries(s.rxRate, s.txRate) : []
  const w = sparklineWindow(s)
  const vsLabel = windowLabel(w.span)
  const directPct = o ? percentOf(o.direct, o.direct + o.relayed) : null
  const latencyDelta = s ? deltaVsStart(s.latency) : null
  const onlineDelta = s ? deltaVsStart(s.online) : null
  const showUnauthorized = !!o && o.unauthorized > 0

  return (
    <div className="space-y-3">
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4 xl:gap-4">
        <StatTile
          label="Devices online"
          loading={loading}
          value={o ? formatInt(o.online) : '—'}
          unit={o ? `/ ${formatInt(o.devices)}` : undefined}
          delta={onlineDelta !== null && onlineDelta !== 0 ? { value: onlineDelta, label: vsLabel, format: (v) => `${v > 0 ? '+' : ''}${formatInt(v)}` } : undefined}
          hint={o ? (o.offline ? `${plural(o.offline, 'device')} offline` : 'Everything is online') : undefined}
          trend={s?.online}
          tone="online"
          icon={MonitorSmartphone}
          to="/devices?status=online"
        />
        <StatTile
          label="Direct paths"
          loading={loading}
          value={formatPercent(directPct)}
          hint={o ? `${formatInt(o.direct)} direct · ${formatInt(o.relayed)} relayed` : undefined}
          tone="direct"
          icon={Waypoints}
          to="/devices?path=direct"
        />
        <StatTile
          label="Throughput now"
          loading={loading}
          value={o ? formatBitrate(o.totalRxRate + o.totalTxRate) : '—'}
          hint={o ? `↓${formatBitrate(o.totalRxRate, 0)} ↑${formatBitrate(o.totalTxRate, 0)}` : undefined}
          trend={total}
          tone="accent"
          icon={Activity}
        />
        <StatTile
          label="Avg latency"
          loading={loading}
          value={formatLatency(o?.avgLatencyMs)}
          delta={latencyDelta !== null ? { value: latencyDelta, label: vsLabel, upIsGood: false, format: (v) => `${v > 0 ? '+' : v < 0 ? '−' : ''}${formatLatency(Math.abs(v))}` } : undefined}
          hint={o ? 'across online devices' : undefined}
          trend={s?.latency}
          tone="info"
          icon={Timer}
        />
      </div>
      <div className={cn('grid grid-cols-2 gap-3 xl:gap-4', showUnauthorized ? 'sm:grid-cols-3 xl:grid-cols-5' : 'lg:grid-cols-4')}>
        <StatTile
          size="sm"
          label="Open alerts"
          loading={loading}
          value={o ? formatInt(o.openAlerts) : '—'}
          hint={
            o ? (
              o.criticalAlerts ? (
                <span className="font-medium text-critical">{plural(o.criticalAlerts, 'critical')}</span>
              ) : o.openAlerts ? (
                'none critical'
              ) : (
                'all clear'
              )
            ) : undefined
          }
          tone={o?.criticalAlerts ? 'critical' : o?.openAlerts ? 'warning' : 'online'}
          icon={Bell}
          to="/alerts"
        />
        <StatTile
          size="sm"
          label="Agents reachable"
          loading={loading}
          value={o ? formatInt(o.agentsReachable) : '—'}
          unit={o ? `/ ${formatInt(o.devices)}` : undefined}
          hint="reporting system metrics"
          tone="accent"
          icon={Radio}
          to="/devices?flags=agent"
        />
        <StatTile
          size="sm"
          label="Updates pending"
          loading={loading}
          value={o ? formatInt(o.updatesPending) : '—'}
          hint={o?.updatesPending ? 'newer client available' : 'clients up to date'}
          tone={o?.updatesPending ? 'info' : 'neutral'}
          icon={Download}
          to="/devices?flags=update"
        />
        <StatTile
          size="sm"
          label="Keys expiring"
          loading={loading}
          value={o ? formatInt(o.keysExpiringSoon) : '—'}
          hint="within 7 days"
          tone={o?.keysExpiringSoon ? 'warning' : 'neutral'}
          icon={KeyRound}
          to="/devices?flags=keyExpiring"
        />
        {showUnauthorized && o ? (
          <StatTile
            size="sm"
            label="Unauthorized"
            value={formatInt(o.unauthorized)}
            hint="awaiting approval"
            tone="warning"
            icon={ShieldAlert}
            to="/devices?flags=unauthorized"
            className="col-span-2 border-warning/40 xl:col-span-1"
          />
        ) : null}
      </div>
    </div>
  )
}
