import { ShieldAlert } from 'lucide-react'
import type { AlertRule } from '../../api/types'
import { cn } from '../../lib/cn'
import { formatDateTime, formatRelative } from '../../lib/format'
import { severityLabel, severityTone } from '../../lib/status'
import { Badge, TONE_SOFT_BG, TONE_TEXT } from '../ui/Badge'
import { EmptyState } from '../ui/EmptyState'
import { ErrorState } from '../ui/ErrorState'
import { SkeletonTable } from '../ui/Skeleton'
import { TBody, TD, TH, THead, TR, Table, sortRows, useSort } from '../ui/Table'
import { Toggle } from '../ui/Toggle'
import { Tooltip } from '../ui/Tooltip'
import { RULE_TYPE_ORDER, formatForSeconds, formatThreshold, ruleIcon, ruleMeta, ruleScopeSummary } from './alerts'

export interface RulesTableProps {
  rules: AlertRule[] | undefined
  loading: boolean
  error: unknown
  onRetry: () => void
  isAdmin: boolean
  onEdit: (rule: AlertRule) => void
  /** Inline toggle (enabled / notify) — admins only. */
  onToggle: (rule: AlertRule, patch: Pick<Partial<AlertRule>, 'enabled' | 'notify'>) => void
  pendingId: string | null
  deviceName?: (id: string) => string | undefined
}

const COLS = 8

/** Every rule as a sortable row; click opens the editor, switches toggle inline for admins. */
export function RulesTable({ rules, loading, error, onRetry, isAdmin, onEdit, onToggle, pendingId, deviceName }: RulesTableProps) {
  const { sort, toggle } = useSort(null)
  if (error) return <ErrorState error={error} onRetry={onRetry} />
  if (loading || !rules) return <SkeletonTable rows={8} cols={6} />
  if (!rules.length) return <EmptyState icon={ShieldAlert} title="No rules" description="The hub did not return any alert rules." />

  const order = new Map(RULE_TYPE_ORDER.map((t, i) => [t, i]))
  const rows = sort
    ? sortRows(rules, sort, {
        name: (r) => r.name,
        enabled: (r) => r.enabled,
        severity: (r) => ({ critical: 0, warning: 1, info: 2 })[r.severity],
        threshold: (r) => (ruleMeta(r.type).unit === 'none' ? null : r.threshold),
        for: (r) => (ruleMeta(r.type).forLabel ? r.forSeconds : null),
        notify: (r) => r.notify,
        scope: (r) => ruleScopeSummary(r, deviceName),
        updated: (r) => r.updatedAt,
      })
    : [...rules].sort((a, b) => (order.get(a.type) ?? 99) - (order.get(b.type) ?? 99))

  return (
    <Table minWidth={700}>
      <THead>
        <TR>
          {/* Rule takes whatever the fixed-width columns leave; its cells are `truncate` (max-w-0) so long descriptions ellipsize instead of widening the table. */}
          <TH sortKey="name" sort={sort} onSort={toggle} className="w-full min-w-[180px]">
            Rule
          </TH>
          <TH sortKey="enabled" sort={sort} onSort={toggle} align="center" width={90}>
            Enabled
          </TH>
          <TH sortKey="severity" sort={sort} onSort={toggle} width={110}>
            Severity
          </TH>
          <TH sortKey="threshold" sort={sort} onSort={toggle} align="right" width={110} hint="Threshold in the unit of the rule type">
            Threshold
          </TH>
          <TH sortKey="for" sort={sort} onSort={toggle} align="right" width={110} hint="How long the condition must hold">
            For
          </TH>
          <TH sortKey="notify" sort={sort} onSort={toggle} align="center" width={80}>
            Notify
          </TH>
          <TH sortKey="scope" sort={sort} onSort={toggle} width={170} hideBelow="xl">
            Scope
          </TH>
          <TH sortKey="updated" sort={sort} onSort={toggle} align="right" width={120} hideBelow="xl">
            Updated
          </TH>
        </TR>
      </THead>
      <TBody>
        {rows.map((r) => {
          const meta = ruleMeta(r.type)
          const Icon = ruleIcon(r.type)
          const tone = severityTone(r.severity)
          const pending = pendingId === r.id
          const stop = (e: React.SyntheticEvent) => e.stopPropagation()
          return (
            <TR key={r.id} onClick={() => onEdit(r)} aria-label={`${r.name}: ${isAdmin ? 'edit rule' : 'view rule'}`} className={cn(!r.enabled && 'text-fg-muted', pending && 'opacity-70')}>
              {/* `truncate` (max-w-0) keeps the long description out of the auto-layout width so the column takes the leftover space and ellipsizes. */}
              <TD truncate>
                <div className="flex min-w-0 items-center gap-3">
                  <span className={cn('flex size-8 shrink-0 items-center justify-center rounded-md', r.enabled ? cn(TONE_SOFT_BG[tone], TONE_TEXT[tone]) : 'bg-surface-inset text-fg-muted')}>
                    <Icon className="size-4" aria-hidden="true" />
                  </span>
                  <div className="min-w-0">
                    <p className={cn('truncate text-[13px] font-medium', r.enabled ? 'text-fg' : 'text-fg-secondary')}>{r.name}</p>
                    <p className="truncate text-xs text-fg-muted" title={r.description || meta.label}>
                      {r.description || meta.label}
                    </p>
                  </div>
                </div>
              </TD>
              <TD align="center">
                <span onClick={stop} onKeyDown={stop} className="inline-flex">
                  <Toggle size="sm" checked={r.enabled} disabled={!isAdmin || pending} onCheckedChange={(enabled) => onToggle(r, { enabled })} aria-label={`${r.name}: enabled`} />
                </span>
              </TD>
              <TD>
                <Badge size="sm" tone={r.enabled ? tone : 'neutral'} dot>
                  {severityLabel(r.severity)}
                </Badge>
              </TD>
              <TD numeric className={meta.unit === 'none' ? 'text-fg-faint' : ''}>
                {formatThreshold(r)}
              </TD>
              <TD numeric className={meta.forLabel ? '' : 'text-fg-faint'}>
                {meta.forLabel ? formatForSeconds(r.forSeconds) : '—'}
              </TD>
              <TD align="center">
                <span onClick={stop} onKeyDown={stop} className="inline-flex">
                  <Toggle size="sm" checked={r.notify} disabled={!isAdmin || pending} onCheckedChange={(notify) => onToggle(r, { notify })} aria-label={`${r.name}: notifications`} />
                </span>
              </TD>
              <TD hideBelow="xl" muted className="max-w-[170px]">
                <span className="block truncate" title={ruleScopeSummary(r, deviceName)}>
                  {ruleScopeSummary(r, deviceName)}
                </span>
              </TD>
              <TD numeric hideBelow="xl" muted>
                <Tooltip content={formatDateTime(r.updatedAt)}>
                  <time dateTime={r.updatedAt} tabIndex={0} className="rounded focus-ring">
                    {formatRelative(r.updatedAt)}
                  </time>
                </Tooltip>
              </TD>
            </TR>
          )
        })}
      </TBody>
    </Table>
  )
}

export const RULES_TABLE_COLS = COLS
