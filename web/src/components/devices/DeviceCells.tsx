// Table/card cells shared by the Devices page and the Overview "Devices" card.
// Every API string renders as text; colour is never the only status signal.

import { Link } from 'react-router-dom'
import { ArrowDown, ArrowUp, KeyRound } from 'lucide-react'
import type { Device } from '../../api/types'
import { cn } from '../../lib/cn'
import { formatBitrate, formatDateTime, formatLatency, formatRelative, formatUptimePct, shortVersion } from '../../lib/format'
import { deviceIcon, osInfo } from '../../lib/os'
import { deviceStatus, pathLabel, pathTone } from '../../lib/status'
import { Badge, TONE_TEXT } from '../ui/Badge'
import { CopyButton } from '../ui/CopyButton'
import { Meter } from '../ui/ProgressBar'
import { Sparkline } from '../ui/Sparkline'
import { StatusDot } from '../ui/StatusDot'
import { TagList } from '../ui/Tag'
import { keyExpiryDays } from './deviceFilters'

export const devicePath = (d: Device) => `/devices/${encodeURIComponent(d.id)}`

/** IPv4 first, then whatever is listed. */
export function primaryAddress(d: Device): string | undefined {
  return d.addresses.find((a) => a.includes('.') && !a.includes(':')) ?? d.addresses[0]
}

export interface DeviceNameCellProps {
  device: Device
  /** Single-line layout for compact tables. */
  dense?: boolean
  /** Render the name as a link (cards use the whole card as the link). */
  link?: boolean
  /** Hide the device-form icon tile. */
  plain?: boolean
  /** Show ACL tag chips (default true). */
  showTags?: boolean
  className?: string
}

/** Status dot + device icon + name, with hostname/tags/status text underneath (or inline when dense). */
export function DeviceNameCell({ device, dense, link, plain, showTags = true, className }: DeviceNameCellProps) {
  const st = deviceStatus(device)
  const Icon = deviceIcon(device)
  const hostDiffers = device.hostname && device.hostname.toLowerCase() !== device.name.toLowerCase()
  const statusText = st.tone === 'online' ? null : st.label
  // Dense rows keep the full name and let the status text truncate instead — but a
  // name can be up to 63 chars, so it is capped at the cell width (max-w-full +
  // truncate) rather than painting over the next column.
  const name = <span className={cn('font-medium text-fg', dense ? 'max-w-full shrink-0 truncate' : 'truncate')}>{device.name}</span>
  return (
    <div className={cn('flex min-w-0 items-center gap-2.5', className)}>
      <StatusDot tone={st.tone} label={st.label} size="sm" />
      {!plain ? (
        <span className={cn('flex shrink-0 items-center justify-center rounded-md bg-surface-inset text-fg-secondary', dense ? 'size-6' : 'size-7')} aria-hidden="true">
          <Icon className={dense ? 'size-3.5' : 'size-4'} />
        </span>
      ) : null}
      <div className="min-w-0">
        <div className="flex min-w-0 items-center gap-1.5">
          {link ? (
            <Link to={devicePath(device)} className="min-w-0 truncate rounded font-medium text-fg hover:text-accent-text focus-ring" onClick={(e) => e.stopPropagation()}>
              {device.name}
            </Link>
          ) : (
            name
          )}
          {device.isSelf ? (
            <Badge size="sm" tone="accent" className="shrink-0">
              hub
            </Badge>
          ) : null}
          {device.isExternal ? (
            <Badge size="sm" tone="neutral" className="shrink-0">
              shared
            </Badge>
          ) : null}
          {dense && statusText ? <span className={cn('hidden min-w-0 truncate text-xs sm:inline', TONE_TEXT[st.tone])}>{statusText}</span> : null}
          {dense && showTags && device.tags.length ? <TagList tags={device.tags} size="sm" max={1} className="hidden shrink-0 flex-nowrap 2xl:inline-flex" /> : null}
        </div>
        {!dense ? (
          <div className="mt-0.5 flex min-w-0 items-center gap-1.5 text-xs text-fg-muted">
            {statusText ? <span className={cn('shrink-0', TONE_TEXT[st.tone])}>{statusText}</span> : null}
            {statusText && (hostDiffers || (showTags && device.tags.length)) ? <span aria-hidden="true">·</span> : null}
            {hostDiffers ? <span className="truncate">{device.hostname}</span> : null}
            {showTags && device.tags.length ? <TagList tags={device.tags} size="sm" max={2} className="flex-nowrap" /> : null}
            {!statusText && !hostDiffers && !(showTags && device.tags.length) ? <span className="truncate">{device.userDisplayName ?? device.user}</span> : null}
          </div>
        ) : null}
      </div>
    </div>
  )
}

export function OSCell({ device, dense }: { device: Device; dense?: boolean }) {
  const os = osInfo(device.os)
  const m = device.metrics
  const sub = device.deviceModel ?? (m?.platform ? `${m.platform}${m.platformVersion ? ` ${m.platformVersion}` : ''}` : undefined)
  return (
    <div className={cn('flex min-w-0 items-center gap-2', dense ? 'max-w-[180px]' : 'max-w-[130px]')}>
      <os.icon className="size-4 shrink-0 text-fg-muted" aria-hidden="true" />
      <div className="min-w-0 leading-tight">
        <div className="truncate text-fg">
          {os.label}
          {dense && sub ? <span className="text-fg-muted"> · {sub}</span> : null}
        </div>
        {!dense && sub ? <div className="mt-0.5 truncate text-xs text-fg-muted">{sub}</div> : null}
      </div>
    </div>
  )
}

export function IPCell({ device, alwaysShowCopy }: { device: Device; alwaysShowCopy?: boolean }) {
  const ip = primaryAddress(device)
  if (!ip) return <span className="text-fg-faint">—</span>
  return (
    <div className="flex items-center gap-0.5">
      <span className="font-mono text-xs text-fg">{ip}</span>
      <CopyButton
        value={ip}
        label="Copy IP"
        size="xs"
        className={cn('-my-1 text-fg-muted', !alwaysShowCopy && 'opacity-0 transition-opacity group-hover/row:opacity-100 focus-visible:opacity-100 group-focus-within/row:opacity-100')}
      />
    </div>
  )
}

export function PathBadge({ device, size = 'sm' }: { device: Device; size?: 'sm' | 'md' }) {
  const p = device.online ? device.connectivity.path : 'none'
  return (
    <Badge tone={pathTone(p)} dot size={size} mono={p === 'relay'}>
      {pathLabel(p, device.connectivity.relay)}
    </Badge>
  )
}

export function LatencyCell({ device }: { device: Device }) {
  const ms = device.online ? device.connectivity.latencyMs : undefined
  if (ms === undefined || ms === null) return <span className="text-fg-faint">—</span>
  return (
    <span className={cn('num whitespace-nowrap', ms >= 250 ? 'text-warning' : 'text-fg')} title={ms >= 250 ? 'Above the 250 ms high-latency threshold' : undefined}>
      {formatLatency(ms)}
    </span>
  )
}

export interface RateCellProps {
  device: Device
  /** Recent rx+tx totals for a tiny live sparkline. */
  history?: ReadonlyArray<number>
  /** Put rx/tx side by side from the sm breakpoint (stacked on two lines otherwise). */
  inline?: boolean
  /** Left-align (cards); default right-aligned for numeric table cells. */
  start?: boolean
}

export function RateCell({ device, history, inline, start }: RateCellProps) {
  if (!device.online) return <span className="text-fg-faint">—</span>
  const { rxRate, txRate } = device.connectivity
  const spark = history && history.length >= 3 ? <Sparkline data={history} width={40} height={inline ? 14 : 20} showLast={false} strokeWidth={1.25} ariaLabel="Recent throughput" /> : null
  return (
    <div className={cn('flex items-center gap-2', start ? 'justify-start' : 'justify-end', inline && 'sm:gap-3')}>
      {spark}
      <div className={cn('num flex flex-col text-xs leading-4', start ? 'items-start' : 'items-end', inline && 'sm:flex-row sm:items-center sm:gap-2')}>
        <span className="flex items-center justify-end gap-0.5 whitespace-nowrap text-fg">
          <ArrowDown className="size-3 text-fg-faint" aria-hidden="true" />
          <span className="sr-only">Receive </span>
          {formatBitrate(rxRate)}
        </span>
        <span className="flex items-center justify-end gap-0.5 whitespace-nowrap text-fg-secondary">
          <ArrowUp className="size-3 text-fg-faint" aria-hidden="true" />
          <span className="sr-only">Transmit </span>
          {formatBitrate(txRate)}
        </span>
      </div>
    </div>
  )
}

export function MeterCell({ value, label, width = 92 }: { value: number | undefined; label: string; width?: number }) {
  if (value === undefined || value === null || !Number.isFinite(value)) return <span className="text-fg-faint">—</span>
  return (
    <div style={{ width }} className="shrink-0">
      <Meter value={value} label={label} size="xs" showValue />
    </div>
  )
}

export function UptimeCell({ device }: { device: Device }) {
  const pct = device.uptime.pct24h
  if (pct === undefined || pct === null) return <span className="text-fg-faint">—</span>
  return <span className={cn('num', pct < 99 ? 'text-warning' : 'text-fg')}>{formatUptimePct(pct)}</span>
}

export function VersionCell({ device }: { device: Device }) {
  return (
    <span className="inline-flex items-center gap-1.5 whitespace-nowrap">
      <span className={cn('font-mono text-xs', device.clientVersion ? 'text-fg' : 'text-fg-faint')}>{shortVersion(device.clientVersion)}</span>
      {device.updateAvailable ? (
        <Badge size="sm" tone="info">
          Update
        </Badge>
      ) : null}
    </span>
  )
}

export function KeyExpiryCell({ device, now }: { device: Device; now?: number }) {
  if (device.expired) {
    return (
      <Badge size="sm" tone="critical" icon={KeyRound}>
        Expired
      </Badge>
    )
  }
  if (device.keyExpiryDisabled) return <span className="text-fg-muted">Never</span>
  const days = keyExpiryDays(device, now)
  if (days === null) return <span className="text-fg-faint">—</span>
  const critical = days < 2
  const warn = days <= 7
  return (
    <span
      className={cn('inline-flex items-center gap-1 whitespace-nowrap num', critical ? 'text-critical' : warn ? 'text-warning' : 'text-fg-secondary')}
      title={formatDateTime(device.keyExpiry)}
    >
      {warn ? <KeyRound className="size-3.5 shrink-0" aria-hidden="true" /> : null}
      {formatRelative(device.keyExpiry, now)}
    </span>
  )
}

export function LastSeenCell({ device, now }: { device: Device; now?: number }) {
  return (
    <span className={cn('whitespace-nowrap num', device.online ? 'text-fg-secondary' : 'text-fg')} title={formatDateTime(device.lastSeen)}>
      {formatRelative(device.lastSeen, now)}
    </span>
  )
}
