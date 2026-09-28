import type { KeyboardEvent, ReactNode } from 'react'
import { MonitorSmartphone, SearchX } from 'lucide-react'
import type { Device } from '../../api/types'
import { cn } from '../../lib/cn'
import { Button } from '../ui/Button'
import { EmptyState } from '../ui/EmptyState'
import { Skeleton } from '../ui/Skeleton'
import { TBody, TD, TH, THead, TR, Table, TableMessage, type SortState } from '../ui/Table'
import { DeviceNameCell, IPCell, KeyExpiryCell, LastSeenCell, LatencyCell, MeterCell, OSCell, PathBadge, RateCell, UptimeCell, VersionCell, devicePath } from './DeviceCells'
import type { ColumnDef, ColumnId, Density } from './columns'

export interface DevicesTableProps {
  devices: ReadonlyArray<Device>
  /** Total before filtering — distinguishes "no devices" from "no matches". */
  total: number
  columns: ReadonlyArray<ColumnDef>
  /** Apply responsive auto-hide classes. */
  autoColumns: boolean
  density: Density
  sort: SortState
  onSort: (key: string) => void
  history: ReadonlyMap<string, ReadonlyArray<number>>
  loading?: boolean
  now?: number
  onClearFilters?: () => void
}

/** Roving focus between rows with the arrow keys; Enter/Space on a row is handled by TR. */
export function rowKeyNav(e: KeyboardEvent<HTMLElement>, selector = 'tr[tabindex]') {
  if (!['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(e.key)) return
  const rows = Array.from(e.currentTarget.querySelectorAll<HTMLElement>(selector))
  const idx = rows.indexOf(e.target as HTMLElement)
  if (idx < 0) return
  e.preventDefault()
  const next = e.key === 'ArrowDown' ? Math.min(idx + 1, rows.length - 1) : e.key === 'ArrowUp' ? Math.max(idx - 1, 0) : e.key === 'Home' ? 0 : rows.length - 1
  rows[next]?.focus()
}

const STICKY_TH = 'sticky left-0 z-[2] bg-surface shadow-[inset_-1px_0_0_var(--border)]'
const STICKY_TD = 'sticky left-0 z-[1] bg-surface shadow-[inset_-1px_0_0_var(--border)] transition-colors group-hover/row:bg-surface-hover'

function cell(id: ColumnId, d: Device, dense: boolean, history: ReadonlyMap<string, ReadonlyArray<number>>, now?: number): ReactNode {
  switch (id) {
    case 'name':
      return <DeviceNameCell device={d} dense={dense} />
    case 'os':
      return <OSCell device={d} dense={dense} />
    case 'ip':
      return <IPCell device={d} />
    case 'user':
      return (
        <span className="block max-w-[180px] truncate text-fg-secondary" title={d.user}>
          {d.userDisplayName ?? d.user}
        </span>
      )
    case 'path':
      return <PathBadge device={d} />
    case 'latency':
      return <LatencyCell device={d} />
    case 'throughput':
      return <RateCell device={d} history={history.get(d.id)} />
    case 'cpu':
      return <MeterCell value={d.metrics?.cpuPercent} label={`CPU usage on ${d.name}`} width={72} />
    case 'mem':
      return <MeterCell value={d.metrics?.memPercent} label={`Memory usage on ${d.name}`} width={72} />
    case 'disk':
      return <MeterCell value={d.metrics?.diskPercent} label={`Disk usage on ${d.name}`} width={72} />
    case 'uptime':
      return <UptimeCell device={d} />
    case 'version':
      return <VersionCell device={d} />
    case 'keyExpiry':
      return <KeyExpiryCell device={d} now={now} />
    case 'lastSeen':
      return <LastSeenCell device={d} now={now} />
  }
}

export function DevicesTable({ devices, total, columns, autoColumns, density, sort, onSort, history, loading, now, onClearFilters }: DevicesTableProps) {
  const dense = density === 'compact'
  const colSpan = columns.length
  const visClass = (c: ColumnDef) => (autoColumns ? c.autoClass : undefined)

  return (
    <Table dense={dense} minWidth={0} stickyHeader maxHeight="calc(100dvh - 240px)" containerClassName="@container min-h-[240px]" aria-label="Devices" aria-rowcount={devices.length}>
      <THead>
        <TR>
          {columns.map((c) => (
            <TH
              key={c.id}
              sortKey={c.sortKey}
              sort={sort}
              onSort={onSort}
              align={c.align}
              hint={c.hint}
              className={cn(visClass(c), c.id === 'name' && STICKY_TH, c.id === 'name' && 'min-w-[200px]')}
            >
              {dense && c.short ? c.short : c.label}
            </TH>
          ))}
        </TR>
      </THead>
      <TBody onKeyDown={(e) => rowKeyNav(e)}>
        {loading && !devices.length
          ? Array.from({ length: 8 }, (_, r) => (
              <tr key={r} aria-hidden="true">
                {columns.map((c) => (
                  <TD key={c.id} className={cn(visClass(c), c.id === 'name' && STICKY_TD)}>
                    <Skeleton height={12} width={c.id === 'name' ? 160 : c.id === 'ip' ? 90 : 56} />
                  </TD>
                ))}
              </tr>
            ))
          : null}
        {!loading && !devices.length ? (
          <TableMessage colSpan={colSpan} className="py-0">
            {total === 0 ? (
              <EmptyState bordered={false} icon={MonitorSmartphone} title="No devices yet" description="Devices appear here once the hub has polled your tailnet." />
            ) : (
              <EmptyState
                bordered={false}
                icon={SearchX}
                title="No devices match"
                description="Try a different search or clear some filters."
                action={
                  onClearFilters ? (
                    <Button size="sm" variant="secondary" onClick={onClearFilters}>
                      Clear filters
                    </Button>
                  ) : undefined
                }
              />
            )}
          </TableMessage>
        ) : null}
        {devices.map((d, i) => (
          <TR key={d.id} to={devicePath(d)} aria-rowindex={i + 1} aria-label={`${d.name}, open details`}>
            {columns.map((c) => (
              <TD
                key={c.id}
                numeric={c.align === 'right'}
                className={cn(
                  visClass(c),
                  c.id === 'name' && cn(STICKY_TD, dense ? 'max-w-[300px]' : 'max-w-[260px]'),
                  (c.id === 'path' || c.id === 'version' || c.id === 'keyExpiry' || c.id === 'ip') && 'whitespace-nowrap',
                )}
              >
                {cell(c.id, d, dense, history, now)}
              </TD>
            ))}
          </TR>
        ))}
      </TBody>
    </Table>
  )
}
