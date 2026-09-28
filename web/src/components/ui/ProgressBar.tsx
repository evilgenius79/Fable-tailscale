import { cn } from '../../lib/cn'
import { formatPercent } from '../../lib/format'
import { utilizationTone, type StatusTone } from '../../lib/status'
import { TONE_FILL_BG, TONE_SOFT_BG, TONE_TEXT } from './Badge'

export interface MeterProps {
  /** Current value in [0, max]. */
  value: number | null | undefined
  max?: number
  /** 'auto' picks accent → warning → critical by thresholds. */
  tone?: 'auto' | StatusTone
  thresholds?: { warning: number; critical: number }
  size?: 'xs' | 'sm' | 'md'
  /** Accessible name, e.g. "CPU usage". */
  label: string
  /** Show the formatted value to the right. */
  showValue?: boolean
  format?: (value: number) => string
  className?: string
}

const H = { xs: 'h-1', sm: 'h-1.5', md: 'h-2' }

/**
 * Utilisation meter: the fill carries severity and the track is a lighter step
 * of the same ramp, so state reads across the whole bar (dataviz meter rule).
 */
export function Meter({ value, max = 100, tone = 'auto', thresholds = { warning: 75, critical: 90 }, size = 'sm', label, showValue, format, className }: MeterProps) {
  const v = value === null || value === undefined || !Number.isFinite(value) ? null : Math.max(0, Math.min(max, value))
  const pct = v === null ? 0 : (v / max) * 100
  const t: StatusTone = tone === 'auto' ? utilizationTone(v === null ? null : pct, thresholds.warning, thresholds.critical) : tone
  const text = v === null ? '—' : format ? format(v) : formatPercent(pct)
  return (
    <div className={cn('flex items-center gap-2', className)}>
      <div
        role="meter"
        aria-label={label}
        aria-valuemin={0}
        aria-valuemax={max}
        aria-valuenow={v ?? undefined}
        aria-valuetext={text}
        className={cn('relative w-full min-w-8 overflow-hidden rounded-full', H[size], v === null ? 'bg-surface-inset' : TONE_SOFT_BG[t])}
      >
        <div className={cn('absolute inset-y-0 left-0 rounded-full transition-[width] duration-300', TONE_FILL_BG[t])} style={{ width: `${pct}%` }} />
      </div>
      {showValue ? <span className={cn('w-10 shrink-0 text-right text-xs num', v === null ? 'text-fg-faint' : TONE_TEXT[t])}>{text}</span> : null}
    </div>
  )
}

export interface ProgressBarProps {
  /** 0–100; omit for indeterminate. */
  value?: number
  label: string
  size?: 'xs' | 'sm' | 'md'
  className?: string
}

/** Task progress (accent). Indeterminate when `value` is omitted. */
export function ProgressBar({ value, label, size = 'sm', className }: ProgressBarProps) {
  const indeterminate = value === undefined
  return (
    <div
      role="progressbar"
      aria-label={label}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={indeterminate ? undefined : Math.round(value)}
      className={cn('relative w-full overflow-hidden rounded-full bg-accent-soft', H[size], className)}
    >
      <div
        className={cn('absolute inset-y-0 rounded-full bg-accent transition-[width] duration-300', indeterminate && 'w-1/3 animate-[shimmer-bar_1.2s_ease-in-out_infinite]')}
        style={indeterminate ? undefined : { width: `${Math.max(0, Math.min(100, value))}%` }}
      />
      {indeterminate ? (
        <style>{`@keyframes shimmer-bar{0%{left:-33%}100%{left:100%}}`}</style>
      ) : null}
    </div>
  )
}
