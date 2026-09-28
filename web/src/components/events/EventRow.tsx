import { Link } from 'react-router-dom'
import type { Event } from '../../api/types'
import { cn } from '../../lib/cn'
import { formatDateTime, formatISO, formatRelative, formatTime } from '../../lib/format'
import { eventTone, eventTypeLabel, severityLabel } from '../../lib/status'
import { Badge, TONE_SOFT_BG, TONE_TEXT } from '../ui/Badge'
import { Tooltip } from '../ui/Tooltip'
import { eventIcon } from './events'

export interface EventRowProps {
  event: Event
  /** Highlight as freshly arrived. */
  fresh?: boolean
  /** Reference time for relative labels (one per render keeps rows consistent). */
  now: number
}

/** One timeline row: icon tile (tone by type), title, message, type/device/time meta. Exact timestamp on hover/focus. */
export function EventRow({ event: e, fresh, now }: EventRowProps) {
  const tone = eventTone(e.type, e.severity)
  const Icon = eventIcon(e.type)
  const exact = (
    <div className="space-y-0.5">
      <p>{formatDateTime(e.ts)}</p>
      <p className="font-mono text-[11px] text-fg-muted">{formatISO(e.ts)}</p>
    </div>
  )
  return (
    <li
      className={cn(
        'relative flex items-start gap-3 px-4 py-3 transition-colors duration-700 sm:px-5',
        fresh && 'bg-accent-soft/60',
      )}
      data-event-id={e.id}
    >
      <span
        className={cn('mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-md', TONE_SOFT_BG[tone], TONE_TEXT[tone])}
        title={eventTypeLabel(e.type)}
      >
        <Icon className="size-4" aria-hidden="true" />
        <span className="sr-only">{eventTypeLabel(e.type)}</span>
      </span>
      <div className="min-w-0 flex-1">
        <div className="flex items-start justify-between gap-3">
          <p className="min-w-0 break-words text-[13px] font-medium leading-5 text-fg">
            {e.title}
            {fresh ? (
              <span className="ml-2 inline-flex items-center rounded-md bg-accent-soft px-1.5 py-0.5 align-middle text-[10px] font-semibold uppercase tracking-wider text-accent-text">New</span>
            ) : null}
          </p>
          <Tooltip content={exact} side="left">
            <time dateTime={e.ts} className="num shrink-0 whitespace-nowrap rounded text-xs text-fg-muted focus-ring" tabIndex={0}>
              <span className="hidden sm:inline">{formatTime(e.ts)}</span>
              <span className="sm:hidden">{formatRelative(e.ts, now)}</span>
            </time>
          </Tooltip>
        </div>
        {e.message ? <p className="mt-0.5 break-words text-[13px] leading-5 text-fg-secondary">{e.message}</p> : null}
        <div className="mt-1.5 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-fg-muted">
          <Badge size="sm" tone={tone} variant="outline">
            {eventTypeLabel(e.type)}
          </Badge>
          {e.severity !== 'info' ? (
            <Badge size="sm" tone={e.severity === 'critical' ? 'critical' : 'warning'} dot>
              {severityLabel(e.severity)}
            </Badge>
          ) : null}
          {e.deviceId && e.deviceName ? (
            <Link to={`/devices/${encodeURIComponent(e.deviceId)}`} className="max-w-[200px] truncate rounded font-medium text-fg-secondary hover:text-accent-text focus-ring">
              {e.deviceName}
            </Link>
          ) : null}
          <span className="num hidden sm:inline" aria-hidden="true">
            · {formatRelative(e.ts, now)}
          </span>
        </div>
      </div>
    </li>
  )
}
