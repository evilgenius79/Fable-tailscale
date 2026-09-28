import { Link } from 'react-router-dom'
import { Check, CheckCheck } from 'lucide-react'
import type { Alert } from '../../api/types'
import { cn } from '../../lib/cn'
import { formatDateTime, formatDuration, formatRelative } from '../../lib/format'
import { severityLabel, severityTone } from '../../lib/status'
import { Badge, TONE_FILL_BG, TONE_SOFT_BG, TONE_TEXT } from '../ui/Badge'
import { Button } from '../ui/Button'
import { Tooltip } from '../ui/Tooltip'
import { alertDurationSeconds, formatAlertValue, ruleIcon, ruleMeta } from './alerts'

export interface AlertRowProps {
  alert: Alert
  isAdmin: boolean
  acking: boolean
  onAck: (alert: Alert) => void
  now: number
}

function Time({ label, ts, now }: { label: string; ts: string; now: number }) {
  return (
    <span className="inline-flex items-center gap-1 whitespace-nowrap">
      <span>{label}</span>
      <time dateTime={ts} title={formatDateTime(ts)} className="num text-fg-secondary">
        {formatRelative(ts, now)}
      </time>
    </span>
  )
}

/**
 * One alert: severity stripe + rule icon, title, message, device link, lifecycle
 * times, measured value and acked-by chip. Admins get an Ack button; viewers a
 * disabled one with an explanation.
 */
export function AlertRow({ alert: a, isAdmin, acking, onAck, now }: AlertRowProps) {
  const tone = severityTone(a.severity)
  const meta = ruleMeta(a.ruleType)
  const Icon = ruleIcon(a.ruleType)
  const value = formatAlertValue(a)
  const open = a.state === 'open'
  const acked = !!a.ackedAt
  const exact = alertDurationSeconds(a, now)
  // Whole minutes once past the first minute: "47m", not "47m 1s".
  const duration = exact >= 60 ? Math.floor(exact / 60) * 60 : exact

  const ackButton = open && !acked ? (
    isAdmin ? (
      <Button size="xs" variant="outline" leadingIcon={Check} loading={acking} onClick={() => onAck(a)} aria-label={`Acknowledge: ${a.title}`}>
        Ack
      </Button>
    ) : (
      <Tooltip content="Only admins can acknowledge alerts" side="left">
        <span className="inline-flex" tabIndex={0} aria-label="Acknowledge (admins only)">
          <Button size="xs" variant="outline" leadingIcon={Check} disabled aria-hidden="true" tabIndex={-1}>
            Ack
          </Button>
        </span>
      </Tooltip>
    )
  ) : null

  return (
    <li className={cn('relative flex items-start gap-3 py-3 pl-5 pr-4 sm:pl-6 sm:pr-5', acked && open && 'opacity-80')}>
      <span aria-hidden="true" className={cn('absolute inset-y-2.5 left-0 w-[3px] rounded-r-full', open ? TONE_FILL_BG[tone] : 'bg-online-fill/60')} />
      <span className={cn('mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-md', open ? cn(TONE_SOFT_BG[tone], TONE_TEXT[tone]) : 'bg-surface-inset text-fg-muted')} title={meta.label}>
        <Icon className="size-4" aria-hidden="true" />
        <span className="sr-only">{meta.label}</span>
      </span>
      <div className="min-w-0 flex-1">
        <div className="flex items-start justify-between gap-3">
          <p className="min-w-0 break-words text-[13px] font-medium leading-5 text-fg">{a.title}</p>
          <div className="flex shrink-0 items-center gap-2">
            {value ? (
              <Badge size="sm" tone={open ? tone : 'neutral'} variant={open ? 'soft' : 'outline'} mono>
                {value}
              </Badge>
            ) : null}
            {ackButton}
          </div>
        </div>
        {a.message ? <p className="mt-0.5 break-words text-[13px] leading-5 text-fg-secondary">{a.message}</p> : null}
        <div className="mt-1.5 flex flex-wrap items-center gap-x-2.5 gap-y-1 text-xs text-fg-muted">
          <Badge size="sm" tone={open ? tone : 'online'} dot>
            {open ? severityLabel(a.severity) : 'Resolved'}
          </Badge>
          {a.deviceId && a.deviceName ? (
            <Link to={`/devices/${encodeURIComponent(a.deviceId)}`} className="max-w-[200px] truncate rounded font-medium text-fg-secondary hover:text-accent-text focus-ring">
              {a.deviceName}
            </Link>
          ) : null}
          <span className="hidden text-fg-muted sm:inline">{meta.label}</span>
          <Time label="opened" ts={a.openedAt} now={now} />
          {open && a.updatedAt !== a.openedAt ? <Time label="updated" ts={a.updatedAt} now={now} /> : null}
          {a.resolvedAt ? <Time label="resolved" ts={a.resolvedAt} now={now} /> : null}
          <span className="num whitespace-nowrap" title={open ? 'Open for' : 'Was open for'}>
            {open ? 'open' : 'lasted'} {formatDuration(duration, 2)}
          </span>
          {acked ? (
            <span className="inline-flex max-w-full items-center gap-1 rounded-md bg-online-soft px-1.5 py-0.5 text-online" title={a.ackedAt ? `Acknowledged ${formatDateTime(a.ackedAt)}` : undefined}>
              <CheckCheck className="size-3 shrink-0" aria-hidden="true" />
              <span className="truncate">acked{a.ackedBy ? ` by ${a.ackedBy}` : ''}</span>
              {a.ackedAt ? <span className="num shrink-0 text-online/80">· {formatRelative(a.ackedAt, now)}</span> : null}
            </span>
          ) : null}
        </div>
      </div>
    </li>
  )
}
