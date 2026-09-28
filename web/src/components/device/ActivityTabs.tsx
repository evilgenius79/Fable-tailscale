import { Link } from 'react-router-dom'
import { BellOff, Check, History } from 'lucide-react'
import { useAckAlert, useDeviceEvents } from '../../api/hooks'
import type { Alert, DeviceID, Event } from '../../api/types'
import { cn } from '../../lib/cn'
import { formatDateTime, formatRelative, plural } from '../../lib/format'
import { alertTone, eventTone, eventTypeLabel, severityLabel, severityTone } from '../../lib/status'
import { Badge } from '../ui/Badge'
import { Button, buttonClass } from '../ui/Button'
import { Card, CardHeader } from '../ui/Card'
import { EmptyState } from '../ui/EmptyState'
import { ErrorState } from '../ui/ErrorState'
import { Skeleton } from '../ui/Skeleton'
import { StatusDot } from '../ui/StatusDot'
import { toast } from '../ui/Toast'

// ---------------------------------------------------------------------------
// Events
// ---------------------------------------------------------------------------

function EventRow({ e }: { e: Event }) {
  const tone = eventTone(e.type, e.severity)
  return (
    <li className="flex gap-3 py-3">
      <StatusDot tone={tone} size="sm" label={eventTypeLabel(e.type)} className="mt-1.5" />
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
          <span className="min-w-0 text-[13px] font-medium text-fg">{e.title}</span>
          <Badge size="sm" tone={tone} variant="outline">
            {eventTypeLabel(e.type)}
          </Badge>
          {e.severity !== 'info' ? (
            <Badge size="sm" tone={severityTone(e.severity)}>
              {severityLabel(e.severity)}
            </Badge>
          ) : null}
        </div>
        {e.message ? <p className="mt-0.5 text-[13px] leading-5 text-fg-secondary break-words">{e.message}</p> : null}
      </div>
      <time dateTime={e.ts} title={formatDateTime(e.ts)} className="shrink-0 whitespace-nowrap text-xs text-fg-muted num">
        {formatRelative(e.ts)}
      </time>
    </li>
  )
}

/** Recent events for one device. */
export function DeviceEvents({ id, deviceName }: { id: DeviceID; deviceName: string }) {
  const q = useDeviceEvents(id, 100)
  const events = q.data ?? []
  return (
    <Card>
      <CardHeader
        title="Events"
        description={q.data ? plural(events.length, 'event') : 'Status changes, path changes, alerts and admin actions'}
        icon={History}
        actions={
          <Link to={`/events?device=${encodeURIComponent(id)}`} className={buttonClass({ variant: 'ghost', size: 'xs' })}>
            All events
          </Link>
        }
      />
      {q.error && !q.data ? (
        <ErrorState compact error={q.error} onRetry={() => void q.refetch()} retrying={q.isFetching} />
      ) : q.isPending ? (
        <div className="space-y-3" aria-busy="true" aria-label="Loading events">
          {Array.from({ length: 5 }, (_, i) => (
            <div key={i} className="flex gap-3">
              <Skeleton width={8} height={8} rounded="full" className="mt-1.5" />
              <div className="flex-1 space-y-1.5">
                <Skeleton height={12} width="45%" />
                <Skeleton height={10} width="70%" />
              </div>
            </div>
          ))}
        </div>
      ) : events.length ? (
        <ol className="divide-y divide-border-subtle">
          {events.map((e) => (
            <EventRow key={e.id} e={e} />
          ))}
        </ol>
      ) : (
        <EmptyState size="sm" bordered={false} icon={History} title="No events yet" description={`Nothing has been recorded for ${deviceName} so far.`} />
      )}
    </Card>
  )
}

// ---------------------------------------------------------------------------
// Alerts
// ---------------------------------------------------------------------------

function AlertRow({ a, isAdmin }: { a: Alert; isAdmin: boolean }) {
  const ack = useAckAlert()
  const tone = alertTone(a)
  const acked = !!a.ackedAt
  return (
    <li className={cn('flex flex-wrap items-start gap-3 py-3 sm:flex-nowrap')}>
      <StatusDot tone={tone} size="sm" pulse={a.state === 'open' && !acked} label={severityLabel(a.severity)} className="mt-1.5" />
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
          <span className="text-[13px] font-medium text-fg">{a.title}</span>
          <Badge size="sm" tone={severityTone(a.severity)}>
            {severityLabel(a.severity)}
          </Badge>
          {acked ? (
            <Badge size="sm" tone="neutral" icon={Check}>
              acknowledged{a.ackedBy ? ` by ${a.ackedBy}` : ''}
            </Badge>
          ) : null}
        </div>
        <p className="mt-0.5 text-[13px] leading-5 text-fg-secondary break-words">{a.message}</p>
        <p className="mt-1 text-xs text-fg-muted num">
          Opened <time dateTime={a.openedAt} title={formatDateTime(a.openedAt)}>{formatRelative(a.openedAt)}</time>
          {a.updatedAt !== a.openedAt ? <> · updated {formatRelative(a.updatedAt)}</> : null}
        </p>
      </div>
      {isAdmin && a.state === 'open' && !acked ? (
        <Button
          size="xs"
          variant="outline"
          leadingIcon={Check}
          loading={ack.isPending}
          onClick={() => ack.mutate(a.id, { onSuccess: () => toast.success('Alert acknowledged', 'Notifications are muted until it resolves.') })}
          className="basis-full sm:basis-auto"
        >
          Acknowledge
        </Button>
      ) : null}
    </li>
  )
}

/** Open alerts for one device with acknowledge actions for admins. */
export function DeviceAlerts({ alerts, isAdmin, deviceId, deviceName }: { alerts: Alert[]; isAdmin: boolean; deviceId: DeviceID; deviceName: string }) {
  const open = alerts.filter((a) => a.state === 'open')
  return (
    <Card>
      <CardHeader
        title="Open alerts"
        description={open.length ? plural(open.length, 'alert') : 'Watchdog rules that currently fire for this device'}
        icon={BellOff}
        actions={
          <Link to={`/alerts?device=${encodeURIComponent(deviceId)}`} className={buttonClass({ variant: 'ghost', size: 'xs' })}>
            Alert history
          </Link>
        }
      />
      {open.length ? (
        <ul className="divide-y divide-border-subtle">
          {open.map((a) => (
            <AlertRow key={a.id} a={a} isAdmin={isAdmin} />
          ))}
        </ul>
      ) : (
        <EmptyState size="sm" bordered={false} icon={Check} title="No open alerts" description={`${deviceName} is not tripping any watchdog rule.`} />
      )}
    </Card>
  )
}
