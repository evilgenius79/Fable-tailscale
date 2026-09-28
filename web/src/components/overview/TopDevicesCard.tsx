import { useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { ArrowRight, MonitorSmartphone } from 'lucide-react'
import type { Device } from '../../api/types'
import { Card, CardHeader } from '../ui/Card'
import { EmptyState } from '../ui/EmptyState'
import { ErrorState } from '../ui/ErrorState'
import { SegmentedControl } from '../ui/SegmentedControl'
import { Skeleton } from '../ui/Skeleton'
import { TBody, TD, TH, THead, TR, Table } from '../ui/Table'
import { DeviceNameCell, LatencyCell, MeterCell, PathBadge, RateCell, devicePath } from '../devices/DeviceCells'
import { rowKeyNav } from '../devices/DevicesTable'
import { topDevices, type TopDevicesBy } from './overview'

export interface TopDevicesCardProps {
  devices: Device[] | undefined
  loading: boolean
  error?: unknown
  onRetry?: () => void
  limit?: number
}

const BY_OPTIONS: ReadonlyArray<{ value: TopDevicesBy; label: string }> = [
  { value: 'throughput', label: 'Throughput' },
  { value: 'latency', label: 'Latency' },
]

/** Compact live table of the busiest (or slowest) online devices. */
export function TopDevicesCard({ devices, loading, error, onRetry, limit = 8 }: TopDevicesCardProps) {
  const [by, setBy] = useState<TopDevicesBy>('throughput')
  const rows = useMemo(() => topDevices(devices ?? [], by, limit), [devices, by, limit])
  const onlineCount = devices?.filter((d) => d.online).length ?? 0
  return (
    <Card padding="none">
      <CardHeader
        divider
        title="Devices"
        description={by === 'throughput' ? 'Busiest online devices right now' : 'Highest latency online devices'}
        actions={
          <>
            <SegmentedControl aria-label="Rank devices by" size="xs" options={BY_OPTIONS} value={by} onValueChange={setBy} />
            <Link to="/devices" className="hidden items-center gap-1 rounded px-1 text-xs font-medium text-accent-text hover:underline focus-ring sm:inline-flex">
              All devices
              <ArrowRight className="size-3" aria-hidden="true" />
            </Link>
          </>
        }
      />
      {error && !devices ? (
        <div className="p-4">
          <ErrorState compact error={error} onRetry={onRetry} />
        </div>
      ) : !loading && !rows.length ? (
        <EmptyState size="sm" bordered={false} icon={MonitorSmartphone} title="No devices online" description={devices?.length ? 'Every device is currently offline.' : 'Devices appear once the hub has polled the tailnet.'} />
      ) : (
        <Table bordered={false} dense aria-label="Top devices">
          <THead>
            <TR>
              <TH>Device</TH>
              <TH hideBelow="sm">Path</TH>
              <TH align="right" hideBelow={by === 'throughput' ? 'sm' : undefined}>
                Latency
              </TH>
              <TH align="right" hideBelow={by === 'latency' ? 'sm' : undefined}>
                Throughput
              </TH>
              <TH hideBelow="md" width={120}>
                CPU
              </TH>
            </TR>
          </THead>
          <TBody onKeyDown={(e) => rowKeyNav(e)}>
            {loading && !rows.length
              ? Array.from({ length: 6 }, (_, i) => (
                  <tr key={i} aria-hidden="true">
                    <TD>
                      <Skeleton height={12} width={140} />
                    </TD>
                    <TD hideBelow="sm">
                      <Skeleton height={12} width={60} />
                    </TD>
                    <TD numeric hideBelow={by === 'throughput' ? 'sm' : undefined}>
                      <Skeleton height={12} width={40} className="ml-auto" />
                    </TD>
                    <TD numeric hideBelow={by === 'latency' ? 'sm' : undefined}>
                      <Skeleton height={12} width={72} className="ml-auto" />
                    </TD>
                    <TD hideBelow="md">
                      <Skeleton height={12} width={80} />
                    </TD>
                  </tr>
                ))
              : rows.map((d) => (
                  <TR key={d.id} to={devicePath(d)} aria-label={`${d.name}, open details`}>
                    <TD>
                      <DeviceNameCell device={d} dense plain showTags={false} />
                    </TD>
                    <TD hideBelow="sm" className="whitespace-nowrap">
                      <PathBadge device={d} />
                    </TD>
                    <TD numeric hideBelow={by === 'throughput' ? 'sm' : undefined}>
                      <LatencyCell device={d} />
                    </TD>
                    <TD numeric hideBelow={by === 'latency' ? 'sm' : undefined}>
                      <RateCell device={d} inline />
                    </TD>
                    <TD hideBelow="md">
                      <MeterCell value={d.metrics?.cpuPercent} label={`CPU usage on ${d.name}`} width={100} />
                    </TD>
                  </TR>
                ))}
          </TBody>
        </Table>
      )}
      {devices && onlineCount > rows.length ? (
        <div className="border-t border-border px-4 py-2 text-xs text-fg-muted sm:px-5">
          Showing {rows.length} of {onlineCount} online devices ·{' '}
          <Link to="/devices?status=online" className="rounded font-medium text-accent-text hover:underline focus-ring">
            view all
          </Link>
        </div>
      ) : null}
    </Card>
  )
}
