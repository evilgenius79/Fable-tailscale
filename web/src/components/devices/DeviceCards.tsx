import { Link } from 'react-router-dom'
import { MonitorSmartphone, SearchX } from 'lucide-react'
import type { Device } from '../../api/types'
import { cn } from '../../lib/cn'
import { formatDateTime, formatRelative } from '../../lib/format'
import { Button } from '../ui/Button'
import { EmptyState } from '../ui/EmptyState'
import { Meter } from '../ui/ProgressBar'
import { Skeleton } from '../ui/Skeleton'
import { DeviceNameCell, IPCell, KeyExpiryCell, LatencyCell, OSCell, PathBadge, RateCell, VersionCell, devicePath } from './DeviceCells'
import { rowKeyNav } from './DevicesTable'

export interface DeviceCardsProps {
  devices: ReadonlyArray<Device>
  total: number
  history: ReadonlyMap<string, ReadonlyArray<number>>
  loading?: boolean
  now?: number
  onClearFilters?: () => void
}

function Stat({ label, children, className }: { label: string; children: React.ReactNode; className?: string }) {
  return (
    <div className={cn('min-w-0', className)}>
      <div className="text-[11px] font-medium uppercase tracking-wider text-fg-muted">{label}</div>
      <div className="mt-0.5 text-[13px] text-fg">{children}</div>
    </div>
  )
}

function DeviceCard({ device, history, now }: { device: Device; history?: ReadonlyArray<number>; now?: number }) {
  const m = device.metrics
  return (
    <li>
      <Link
        to={devicePath(device)}
        data-row
        aria-label={`${device.name}, open details`}
        className="group/row block rounded-lg border border-border bg-surface p-3.5 shadow-xs transition-colors hover:border-border-strong hover:bg-surface-hover focus-ring"
      >
        <div className="flex items-start justify-between gap-3">
          <DeviceNameCell device={device} className="min-w-0 flex-1" />
          <div className="shrink-0">
            <PathBadge device={device} />
          </div>
        </div>
        <div className="mt-3 grid grid-cols-2 gap-x-4 gap-y-2.5">
          <Stat label="OS">
            <OSCell device={device} dense />
          </Stat>
          <Stat label="Tailscale IP">
            <IPCell device={device} alwaysShowCopy />
          </Stat>
          <Stat label="User">
            <span className="block truncate text-fg-secondary" title={device.user}>
              {device.userDisplayName ?? device.user}
            </span>
          </Stat>
          <Stat label="Latency">
            <LatencyCell device={device} />
          </Stat>
          <Stat label="Throughput" className="col-span-2">
            <RateCell device={device} history={history} inline start />
          </Stat>
          {m ? (
            <div className="col-span-2 grid grid-cols-3 gap-3">
              <Meter value={m.cpuPercent} label={`CPU usage on ${device.name}`} size="xs" showValue format={(v) => `CPU ${Math.round(v)}%`} className="[&>span]:w-16" />
              <Meter value={m.memPercent} label={`Memory usage on ${device.name}`} size="xs" showValue format={(v) => `Mem ${Math.round(v)}%`} className="[&>span]:w-16" />
              <Meter value={m.diskPercent} label={`Disk usage on ${device.name}`} size="xs" showValue format={(v) => `Disk ${Math.round(v)}%`} className="[&>span]:w-16" />
            </div>
          ) : null}
        </div>
        <div className="mt-3 flex flex-wrap items-center justify-between gap-x-3 gap-y-1 border-t border-border-subtle pt-2.5 text-xs text-fg-muted">
          <span className="inline-flex items-center gap-2">
            <VersionCell device={device} />
            <span aria-hidden="true">·</span>
            <span className="inline-flex items-center gap-1">
              Key <KeyExpiryCell device={device} now={now} />
            </span>
          </span>
          <span title={formatDateTime(device.lastSeen)}>
            {device.online ? 'Seen ' : 'Last seen '}
            <span className="num text-fg-secondary">{formatRelative(device.lastSeen, now)}</span>
          </span>
        </div>
      </Link>
    </li>
  )
}

/** Card layout for narrow screens (below md). Arrow keys move between cards. */
export function DeviceCards({ devices, total, history, loading, now, onClearFilters }: DeviceCardsProps) {
  if (loading && !devices.length) {
    return (
      <ul className="space-y-3" aria-busy="true" aria-label="Loading devices">
        {Array.from({ length: 4 }, (_, i) => (
          <li key={i} className="rounded-lg border border-border bg-surface p-3.5 shadow-xs" aria-hidden="true">
            <Skeleton height={14} width="45%" />
            <div className="mt-3 grid grid-cols-2 gap-3">
              <Skeleton height={12} />
              <Skeleton height={12} />
              <Skeleton height={12} />
              <Skeleton height={12} />
            </div>
          </li>
        ))}
      </ul>
    )
  }
  if (!devices.length) {
    return total === 0 ? (
      <EmptyState icon={MonitorSmartphone} title="No devices yet" description="Devices appear here once the hub has polled your tailnet." />
    ) : (
      <EmptyState
        icon={SearchX}
        title="No devices match"
        description="Try a different search or clear some filters."
        action={
          onClearFilters ? (
            <Button size="sm" onClick={onClearFilters}>
              Clear filters
            </Button>
          ) : undefined
        }
      />
    )
  }
  return (
    <ul className="space-y-3" aria-label="Devices" onKeyDown={(e) => rowKeyNav(e, '[data-row]')}>
      {devices.map((d) => (
        <DeviceCard key={d.id} device={d} history={history.get(d.id)} now={now} />
      ))}
    </ul>
  )
}
