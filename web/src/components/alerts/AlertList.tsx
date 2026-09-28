import { BellOff, CircleCheck, SearchX } from 'lucide-react'
import type { Alert } from '../../api/types'
import { cn } from '../../lib/cn'
import { plural } from '../../lib/format'
import { Button } from '../ui/Button'
import { EmptyState } from '../ui/EmptyState'
import { ErrorState } from '../ui/ErrorState'
import { Skeleton } from '../ui/Skeleton'
import { StatusDot } from '../ui/StatusDot'
import { AlertRow } from './AlertRow'
import type { AlertGroup } from './alerts'

export interface AlertListProps {
  state: 'open' | 'resolved'
  groups: AlertGroup[]
  loading: boolean
  fetching?: boolean
  error: unknown
  onRetry: () => void
  filtering: boolean
  onClearFilters: () => void
  isAdmin: boolean
  ackingId: number | null
  onAck: (alert: Alert) => void
  now: number
}

function SkeletonRows({ n = 4 }: { n?: number }) {
  return (
    <ul className="surface-card divide-y divide-border-subtle" aria-busy="true" aria-label="Loading alerts">
      {Array.from({ length: n }, (_, i) => (
        <li key={i} className="flex items-start gap-3 px-5 py-3" aria-hidden="true">
          <Skeleton width={32} height={32} rounded="md" />
          <div className="flex-1 space-y-2 pt-1">
            <Skeleton height={12} width={`${50 + ((i * 17) % 35)}%`} />
            <Skeleton height={10} width="80%" />
            <Skeleton height={10} width="40%" />
          </div>
        </li>
      ))}
    </ul>
  )
}

/** Grouped alert list (by severity for open alerts, by day for resolved ones). */
export function AlertList({ state, groups, loading, fetching, error, onRetry, filtering, onClearFilters, isAdmin, ackingId, onAck, now }: AlertListProps) {
  if (error) return <ErrorState error={error} onRetry={onRetry} />
  if (loading) return <SkeletonRows />
  if (!groups.length) {
    if (filtering) {
      return (
        <EmptyState
          icon={SearchX}
          title="No alerts match"
          description="Try another severity or device, or clear the search."
          action={
            <Button size="sm" onClick={onClearFilters}>
              Clear filters
            </Button>
          }
        />
      )
    }
    return state === 'open' ? (
      <EmptyState icon={CircleCheck} title="No open alerts" description="Every watchdog rule is satisfied. New alerts appear here the moment they open." />
    ) : (
      <EmptyState icon={BellOff} title="Nothing resolved yet" description="Alerts move here once their condition clears." />
    )
  }
  return (
    <div className={cn('space-y-5 transition-opacity duration-300', fetching && 'opacity-80')}>
      {groups.map((g) => (
        <section key={g.key} aria-labelledby={`alerts-${state}-${g.key}`}>
          <div className="mb-2 flex items-center gap-2 px-1">
            {g.tone !== 'neutral' ? <StatusDot tone={g.tone} size="sm" /> : null}
            <h2 id={`alerts-${state}-${g.key}`} className="text-xs font-semibold uppercase tracking-wider text-fg-secondary">
              {g.label}
            </h2>
            <span className="num text-[11px] text-fg-muted">{plural(g.alerts.length, 'alert')}</span>
            <span className="h-px flex-1 bg-border" aria-hidden="true" />
          </div>
          <ul className="surface-card divide-y divide-border-subtle overflow-hidden">
            {g.alerts.map((a) => (
              <AlertRow key={a.id} alert={a} isAdmin={isAdmin} acking={ackingId === a.id} onAck={onAck} now={now} />
            ))}
          </ul>
        </section>
      ))}
    </div>
  )
}
