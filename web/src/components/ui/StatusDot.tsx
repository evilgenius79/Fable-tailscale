import { cn } from '../../lib/cn'
import type { StatusTone } from '../../lib/status'
import { TONE_FILL_BG, TONE_FILL_TEXT } from './Badge'

export interface StatusDotProps {
  tone: StatusTone
  size?: 'xs' | 'sm' | 'md' | 'lg'
  /** Soft ring animation (live/online). Disabled under prefers-reduced-motion by the global rule. */
  pulse?: boolean
  /** Screen-reader text (strongly recommended; visible text should accompany the dot elsewhere). */
  label?: string
  className?: string
}

const SIZES = { xs: 'size-1.5', sm: 'size-2', md: 'size-2.5', lg: 'size-3' }

/** A coloured status dot. Pair it with visible text — colour is never the only signal. */
export function StatusDot({ tone, size = 'sm', pulse, label, className }: StatusDotProps) {
  return (
    <span className={cn('relative inline-flex shrink-0 items-center justify-center', TONE_FILL_TEXT[tone], className)}>
      <span aria-hidden="true" className={cn('rounded-full', SIZES[size], TONE_FILL_BG[tone], pulse && 'animate-pulse-dot')} />
      {label ? <span className="sr-only">{label}</span> : null}
    </span>
  )
}
