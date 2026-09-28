import { useMemo } from 'react'
import { Link } from 'react-router-dom'
import { ArrowRight, Check, CircleCheck } from 'lucide-react'
import type { Alert } from '../../api/types'
import { useAckAlert, useAlerts, useIsAdmin } from '../../api/hooks'
import { cn } from '../../lib/cn'
import { formatDateTime, formatInt, formatRelative } from '../../lib/format'
import { severityLabel, severityTone } from '../../lib/status'
import { Badge } from '../ui/Badge'
import { Button } from '../ui/Button'
import { Card, CardHeader } from '../ui/Card'
import { EmptyState } from '../ui/EmptyState'
import { ErrorState } from '../ui/ErrorState'
import { Skeleton } from '../ui/Skeleton'
import { toast } from '../ui/Toast'
import { sortOpenAlerts } from './overview'

function AlertRow({ alert: a, canAck, acking, onAck }: { alert: Alert; canAck: boolean; acking: boolean; onAck: () => void }) {
  const tone = severityTone(a.severity)
  return (
    <li className={cn('flex items-start gap-3 px-4 py-3 sm:px-5', a.ackedAt && 'opacity-75')}>
      <div className="min-w-0 flex-1">
        <p className="truncate text-[13px] font-medium leading-5 text-fg" title={a.title}>
          {a.title}
        </p>
        <div className="mt-1 flex flex-wrap items-center gap-x-1.5 gap-y-1 text-xs text-fg-muted">
          <Badge size="sm" tone={tone} dot>
            {severityLabel(a.severity)}
          </Badge>
          {a.deviceId && a.deviceName ? (
            <Link to={`/devices/${encodeURIComponent(a.deviceId)}`} className="max-w-[160px] truncate rounded font-medium text-fg-secondary hover:text-accent-text focus-ring">
              {a.deviceName}
            </Link>
          ) : null}
          <span aria-hidden="true">·</span>
          <time dateTime={a.openedAt} title={formatDateTime(a.openedAt)} className="num whitespace-nowrap">
            {formatRelative(a.openedAt)}
          </time>
          {a.ackedAt ? (
            <span className="inline-flex items-center gap-1 whitespace-nowrap">
              <Check className="size-3 text-online" aria-hidden="true" />
              acked{a.ackedBy ? ` by ${a.ackedBy}` : ''}
            </span>
          ) : null}
        </div>
      </div>
      {canAck && !a.ackedAt ? (
        <Button size="xs" variant="outline" onClick={onAck} loading={acking} aria-label={`Acknowledge: ${a.title}`}>
          Ack
        </Button>
      ) : null}
    </li>
  )
}

/** Open alerts, unacknowledged and most severe first; admins can acknowledge inline. */
export function OpenAlertsCard({ limit = 6 }: { limit?: number }) {
  const q = useAlerts()
  const isAdmin = useIsAdmin()
  const ack = useAckAlert()
  const open = useMemo(() => sortOpenAlerts(q.data ?? []), [q.data])
  const rows = open.slice(0, limit)
  const unacked = open.filter((a) => !a.ackedAt).length
  return (
    <Card padding="none">
      <CardHeader
        divider
        title={
          <span className="inline-flex items-center gap-2">
            Open alerts
            {q.data ? (
              <Badge size="sm" tone={unacked ? (open.some((a) => a.severity === 'critical') ? 'critical' : 'warning') : 'neutral'}>
                {formatInt(open.length)}
              </Badge>
            ) : null}
          </span>
        }
        description={q.data ? (unacked ? `${formatInt(unacked)} need attention` : open.length ? 'All acknowledged' : 'Nothing needs attention') : undefined}
        actions={
          <Link to="/alerts" className="inline-flex items-center gap-1 rounded px-1 text-xs font-medium text-accent-text hover:underline focus-ring">
            All alerts
            <ArrowRight className="size-3" aria-hidden="true" />
          </Link>
        }
      />
      {q.error && !q.data ? (
        <div className="p-4">
          <ErrorState compact error={q.error} onRetry={() => void q.refetch()} retrying={q.isFetching} />
        </div>
      ) : q.isPending ? (
        <ul className="divide-y divide-border-subtle" aria-busy="true" aria-label="Loading alerts">
          {Array.from({ length: 4 }, (_, i) => (
            <li key={i} className="space-y-2 px-4 py-3 sm:px-5" aria-hidden="true">
              <Skeleton height={12} width="70%" />
              <Skeleton height={10} width="45%" />
            </li>
          ))}
        </ul>
      ) : rows.length === 0 ? (
        <EmptyState size="sm" bordered={false} icon={CircleCheck} title="No open alerts" description="Every watchdog rule is satisfied." />
      ) : (
        <ul className="divide-y divide-border-subtle" aria-label="Open alerts">
          {rows.map((a) => (
            <AlertRow
              key={a.id}
              alert={a}
              canAck={isAdmin}
              acking={ack.isPending && ack.variables === a.id}
              onAck={() => ack.mutate(a.id, { onSuccess: () => toast.success('Alert acknowledged', a.title, { duration: 2500 }) })}
            />
          ))}
        </ul>
      )}
      {open.length > rows.length ? (
        <div className="border-t border-border px-4 py-2 text-xs text-fg-muted sm:px-5">
          {formatInt(open.length - rows.length)} more ·{' '}
          <Link to="/alerts" className="rounded font-medium text-accent-text hover:underline focus-ring">
            view all
          </Link>
        </div>
      ) : null}
    </Card>
  )
}
