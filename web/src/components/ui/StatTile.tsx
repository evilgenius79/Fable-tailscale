import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { ArrowDownRight, ArrowUpRight, Minus } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { cn } from '../../lib/cn'
import { formatPercent } from '../../lib/format'
import type { StatusTone } from '../../lib/status'
import { TONE_SOFT_BG, TONE_TEXT } from './Badge'
import { Skeleton } from './Skeleton'
import { Sparkline } from './Sparkline'

export interface StatDelta {
  /** Signed change; formatted with `format` (default: percent with sign). */
  value: number
  /** Comparison label, e.g. "vs 24h ago". */
  label?: string
  /** Whether an increase is good (colours the delta). Default true. */
  upIsGood?: boolean
  format?: (v: number) => string
}

export interface StatTileProps {
  label: ReactNode
  value: ReactNode
  unit?: ReactNode
  delta?: StatDelta
  /** Recent values; drawn as a sparkline in the tile's tone. */
  trend?: ReadonlyArray<number | null | undefined>
  /** Colour for the icon tile and sparkline. */
  tone?: StatusTone | 'default'
  icon?: LucideIcon
  /** Footnote under the value ("of 17 devices"). */
  hint?: ReactNode
  loading?: boolean
  /** Renders the tile as a link. */
  to?: string
  onClick?: () => void
  size?: 'sm' | 'md'
  className?: string
}

const TONE_VAR: Record<StatusTone, string> = {
  online: 'var(--online-fill)',
  offline: 'var(--offline-fill)',
  warning: 'var(--warning-fill)',
  critical: 'var(--critical-fill)',
  relay: 'var(--relay-fill)',
  direct: 'var(--direct-fill)',
  info: 'var(--info-fill)',
  neutral: 'var(--fg-muted)',
  accent: 'var(--accent)',
}

/**
 * KPI tile per the dataviz figure contract: label · value (proportional figures)
 * · optional signed delta vs a named period · optional sparkline.
 */
export function StatTile({ label, value, unit, delta, trend, tone = 'default', icon: Icon, hint, loading, to, onClick, size = 'md', className }: StatTileProps) {
  const t: StatusTone = tone === 'default' ? 'accent' : tone
  const interactive = !!to || !!onClick
  const deltaDir = delta ? (delta.value > 0 ? 'up' : delta.value < 0 ? 'down' : 'flat') : null
  const deltaGood = delta && deltaDir !== 'flat' ? (delta.value > 0) === (delta.upIsGood ?? true) : null
  const DeltaIcon = deltaDir === 'up' ? ArrowUpRight : deltaDir === 'down' ? ArrowDownRight : Minus
  const deltaText = delta ? (delta.format ? delta.format(delta.value) : `${delta.value > 0 ? '+' : ''}${formatPercent(delta.value, 1)}`) : null

  const body = (
    <>
      <div className="flex items-start justify-between gap-3">
        <p className={cn('truncate font-medium text-fg-muted', size === 'sm' ? 'text-xs' : 'text-[13px]')}>{label}</p>
        {Icon ? (
          <span className={cn('flex shrink-0 items-center justify-center rounded-md', size === 'sm' ? 'size-6' : 'size-7', TONE_SOFT_BG[t], TONE_TEXT[t])}>
            <Icon className={size === 'sm' ? 'size-3.5' : 'size-4'} aria-hidden="true" />
          </span>
        ) : null}
      </div>
      <div className={cn('flex items-end justify-between gap-3', size === 'sm' ? 'mt-1.5' : 'mt-2')}>
        <div className="min-w-0">
          {loading ? (
            <Skeleton height={size === 'sm' ? 22 : 28} width={72} className="my-0.5" />
          ) : (
            <p className={cn('truncate font-semibold tracking-tight text-fg', size === 'sm' ? 'text-xl leading-7' : 'text-2xl leading-8')}>
              {value}
              {unit ? <span className="ml-1 text-sm font-medium text-fg-muted">{unit}</span> : null}
            </p>
          )}
          {delta && !loading ? (
            <p className={cn('mt-0.5 flex items-center gap-1 whitespace-nowrap text-xs', deltaGood === null ? 'text-fg-muted' : deltaGood ? 'text-online' : 'text-critical')}>
              <DeltaIcon className="size-3.5 shrink-0" aria-hidden="true" />
              <span className="num font-medium">{deltaText}</span>
              {delta.label ? <span className="truncate text-fg-muted">{delta.label}</span> : null}
            </p>
          ) : hint && !loading ? (
            <p className="mt-0.5 truncate text-xs text-fg-muted">{hint}</p>
          ) : null}
        </div>
        {trend && trend.length > 1 && !loading ? (
          <div className="w-20 shrink-0 md:w-24">
            <Sparkline data={trend} height={size === 'sm' ? 24 : 30} color={TONE_VAR[t]} />
          </div>
        ) : null}
      </div>
    </>
  )

  const cls = cn(
    'block min-w-0 rounded-lg border border-border bg-surface shadow-xs',
    size === 'sm' ? 'p-3' : 'p-4',
    interactive && 'transition-colors hover:border-border-strong hover:bg-surface-hover focus-ring',
    className,
  )
  if (to) {
    return (
      <Link to={to} className={cls}>
        {body}
      </Link>
    )
  }
  if (onClick) {
    return (
      <button type="button" onClick={onClick} className={cn(cls, 'w-full text-left')}>
        {body}
      </button>
    )
  }
  return <div className={cls}>{body}</div>
}
