import { BookOpen, Radio } from 'lucide-react'
import { cn } from '../../lib/cn'
import { severityTone } from '../../lib/status'
import { Badge, TONE_SOFT_BG, TONE_TEXT } from '../ui/Badge'
import { Card, CardHeader } from '../ui/Card'
import { RULE_TYPES, RULE_TYPE_ORDER, formatForSeconds, formatThreshold } from './alerts'

/** Read-only reference: what each rule type watches, its unit, defaults and escalation. */
export function RuleTypeGuide({ className }: { className?: string }) {
  return (
    <Card padding="none" className={className} id="rule-guide">
      <CardHeader divider icon={BookOpen} title="How rules work" description="One open alert per rule and device. It opens while the condition holds, resolves when it clears; acknowledging silences notifications until then." />
      <ul className="grid gap-px bg-border-subtle sm:grid-cols-2 xl:grid-cols-3">
        {RULE_TYPE_ORDER.map((type) => {
          const m = RULE_TYPES[type]
          const Icon = m.icon
          const tone = severityTone(m.defaults.severity)
          return (
            <li key={type} className="flex gap-3 bg-surface px-4 py-3.5 sm:px-5">
              <span className={cn('mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-md', TONE_SOFT_BG[tone], TONE_TEXT[tone])}>
                <Icon className="size-4" aria-hidden="true" />
              </span>
              <div className="min-w-0 space-y-1.5">
                <div className="flex flex-wrap items-center gap-1.5">
                  <p className="text-[13px] font-semibold text-fg">{m.label}</p>
                  <Badge size="sm" tone={tone} dot>
                    {m.defaults.severity}
                  </Badge>
                  {m.needsAgent ? (
                    <Badge size="sm" tone="neutral" icon={Radio}>
                      agent
                    </Badge>
                  ) : null}
                </div>
                <p className="text-xs leading-5 text-fg-secondary">{m.explanation}</p>
                {m.escalation ? <p className="text-xs leading-5 text-warning">{m.escalation}</p> : null}
                <p className="num text-[11px] text-fg-muted">
                  Default: {m.unit !== 'none' ? `${formatThreshold({ type, threshold: m.defaults.threshold })}` : 'no threshold'}
                  {m.forLabel ? ` · ${m.forLabel.toLowerCase()} ${formatForSeconds(m.defaults.forSeconds)}` : ''}
                </p>
              </div>
            </li>
          )
        })}
      </ul>
    </Card>
  )
}
