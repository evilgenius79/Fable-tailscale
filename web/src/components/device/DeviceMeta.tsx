import { Fragment } from 'react'
import { Link } from 'react-router-dom'
import { MapPin, User } from 'lucide-react'
import type { Device } from '../../api/types'
import { cn } from '../../lib/cn'
import { formatRelative, shortVersion } from '../../lib/format'
import { osInfo } from '../../lib/os'
import { deviceStatus } from '../../lib/status'
import { Badge } from '../ui/Badge'
import { CopyButton } from '../ui/CopyButton'
import { StatusDot } from '../ui/StatusDot'
import { Tag } from '../ui/Tag'
import { Tooltip } from '../ui/Tooltip'
import { deviceBadges } from './deviceDetail'

/** Title contents: status dot + name (the h1 truncates the name). */
export function DeviceTitle({ device }: { device: Device }) {
  const st = deviceStatus(device)
  return (
    <span className="inline-flex max-w-full items-center gap-2.5 align-middle">
      <StatusDot tone={st.tone} size="md" pulse={device.online && st.tone === 'online'} label={st.label} />
      <span className="min-w-0 truncate">{device.name}</span>
    </span>
  )
}

/** Subtitle: MagicDNS name with copy, hostname when it differs. */
export function DeviceSubtitle({ device }: { device: Device }) {
  const hostDiffers = device.hostname && device.hostname.toLowerCase() !== device.name.toLowerCase()
  return (
    <span className="inline-flex max-w-full flex-wrap items-center gap-x-1 gap-y-0.5">
      <span className="truncate font-mono text-[13px] text-fg-secondary">{device.dnsName}</span>
      <CopyButton value={device.dnsName} label="Copy DNS name" size="xs" className="-my-1" />
      {hostDiffers ? (
        <span className="text-fg-muted">
          · hostname <span className="font-mono text-[13px] text-fg-secondary">{device.hostname}</span>
        </span>
      ) : null}
    </span>
  )
}

/** Badges (status + roles) and key facts (OS, model, user, IPs, tags) for the header meta row. */
export function DeviceMeta({ device }: { device: Device }) {
  const st = deviceStatus(device)
  const os = osInfo(device.os)
  const OsIcon = os.icon
  const badges = deviceBadges(device)
  const ip4 = device.addresses.find((a) => a.includes('.') && !a.includes(':'))
  const ip6 = device.addresses.find((a) => a.includes(':'))
  return (
    <>
      <span className="flex flex-wrap items-center gap-1.5">
        <Tooltip content={st.detail}>
          <span className="inline-flex">
            <Badge tone={st.tone} dot>
              {st.label}
            </Badge>
          </span>
        </Tooltip>
        {badges.map((b) => (
          <Badge key={b.id} tone={b.tone} variant="outline">
            {b.label}
          </Badge>
        ))}
      </span>
      <span className="hidden h-4 w-px bg-border sm:inline-block" aria-hidden="true" />
      <span className="inline-flex items-center gap-1.5 text-fg-secondary">
        <OsIcon className="size-3.5 text-fg-muted" aria-hidden="true" />
        <span className="sr-only">Operating system </span>
        {os.label}
        {device.clientVersion ? (
          <span className="text-fg-muted num" title={device.clientVersion}>
            · Tailscale {shortVersion(device.clientVersion)}
          </span>
        ) : null}
      </span>
      {device.deviceModel ? <span className="truncate text-fg-secondary">{device.deviceModel}</span> : null}
      <span className="inline-flex items-center gap-1.5 text-fg-secondary">
        <User className="size-3.5 text-fg-muted" aria-hidden="true" />
        <span className="sr-only">Owner </span>
        <Link to={`/devices?user=${encodeURIComponent(device.user)}`} className="rounded hover:text-fg focus-ring">
          {device.userDisplayName ? (
            <>
              {device.userDisplayName} <span className="text-fg-muted">({device.user})</span>
            </>
          ) : (
            device.user
          )}
        </Link>
      </span>
      {device.location?.city || device.location?.country ? (
        <span className="inline-flex items-center gap-1.5 text-fg-secondary">
          <MapPin className="size-3.5 text-fg-muted" aria-hidden="true" />
          {[device.location.city, device.location.country].filter(Boolean).join(', ')}
        </span>
      ) : null}
      {[ip4, ip6].filter(Boolean).map((ip) => (
        <Fragment key={ip}>
          <span className="inline-flex items-center gap-0.5">
            <span className="font-mono text-[12px] text-fg-secondary">{ip}</span>
            <CopyButton value={ip!} label={`Copy ${ip}`} size="xs" className="-my-1" />
          </span>
        </Fragment>
      ))}
      {device.tags.length ? (
        <span className="inline-flex flex-wrap items-center gap-1">
          {device.tags.map((t) => (
            <Tag key={t} tag={t} size="sm" />
          ))}
        </span>
      ) : null}
      {!device.online ? <span className={cn('text-fg-muted num')}>Last seen {formatRelative(device.lastSeen)}</span> : null}
    </>
  )
}
