import { ChevronRight } from 'lucide-react'
import type { AlertRule } from '../../api/types'
import { cn } from '../../lib/cn'
import { severityLabel, severityTone } from '../../lib/status'
import { Badge, TONE_SOFT_BG, TONE_TEXT } from '../ui/Badge'
import { Toggle } from '../ui/Toggle'
import { RULE_TYPE_ORDER, formatForSeconds, formatThreshold, ruleIcon, ruleMeta, ruleScopeSummary } from './alerts'

export interface RuleCardsProps {
  rules: AlertRule[]
  isAdmin: boolean
  onEdit: (rule: AlertRule) => void
  onToggle: (rule: AlertRule, patch: Pick<Partial<AlertRule>, 'enabled' | 'notify'>) => void
  pendingId: string | null
  deviceName?: (id: string) => string | undefined
  className?: string
}

/** Phone layout for the rules list: one card per rule with the switches in reach and a tap-to-edit body. */
export function RuleCards({ rules, isAdmin, onEdit, onToggle, pendingId, deviceName, className }: RuleCardsProps) {
  const order = new Map(RULE_TYPE_ORDER.map((t, i) => [t, i]))
  const rows = [...rules].sort((a, b) => (order.get(a.type) ?? 99) - (order.get(b.type) ?? 99))
  return (
    <ul className={cn('space-y-2', className)} aria-label="Alert rules">
      {rows.map((r) => {
        const meta = ruleMeta(r.type)
        const Icon = ruleIcon(r.type)
        const tone = severityTone(r.severity)
        const pending = pendingId === r.id
        return (
          <li key={r.id} className={cn('surface-card overflow-hidden', pending && 'opacity-70')}>
            <button type="button" onClick={() => onEdit(r)} className="flex w-full items-start gap-3 px-3.5 pt-3.5 pb-2 text-left focus-ring-inset" aria-label={`${r.name}: ${isAdmin ? 'edit rule' : 'view rule'}`}>
              <span className={cn('mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-md', r.enabled ? cn(TONE_SOFT_BG[tone], TONE_TEXT[tone]) : 'bg-surface-inset text-fg-muted')}>
                <Icon className="size-4" aria-hidden="true" />
              </span>
              <span className="min-w-0 flex-1">
                <span className={cn('block text-[13px] font-medium', r.enabled ? 'text-fg' : 'text-fg-secondary')}>{r.name}</span>
                <span className="block text-xs leading-5 text-fg-muted">{r.description || meta.label}</span>
                <span className="mt-1.5 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-fg-muted">
                  <Badge size="sm" tone={r.enabled ? tone : 'neutral'} dot>
                    {severityLabel(r.severity)}
                  </Badge>
                  {meta.unit !== 'none' ? <span className="num">{formatThreshold(r)}</span> : null}
                  {meta.forLabel ? <span className="num">{meta.forLabel.toLowerCase()} {formatForSeconds(r.forSeconds)}</span> : null}
                  <span className="truncate">{ruleScopeSummary(r, deviceName)}</span>
                </span>
              </span>
              <ChevronRight className="mt-2 size-4 shrink-0 text-fg-faint" aria-hidden="true" />
            </button>
            <div className="flex items-center gap-5 border-t border-border-subtle px-3.5 py-2">
              <Toggle size="sm" checked={r.enabled} disabled={!isAdmin || pending} onCheckedChange={(enabled) => onToggle(r, { enabled })} label="Enabled" labelPosition="end" className="items-center" />
              <Toggle size="sm" checked={r.notify} disabled={!isAdmin || pending} onCheckedChange={(notify) => onToggle(r, { notify })} label="Notify" labelPosition="end" className="items-center" />
            </div>
          </li>
        )
      })}
    </ul>
  )
}
