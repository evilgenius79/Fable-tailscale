import type { ComponentProps } from 'react'
import type { LucideIcon } from 'lucide-react'
import { cn } from '../../lib/cn'
import type { StatusTone } from '../../lib/status'

export type BadgeTone = StatusTone
export type BadgeVariant = 'soft' | 'outline' | 'solid'

export const TONE_TEXT: Record<BadgeTone, string> = {
  online: 'text-online',
  offline: 'text-offline',
  warning: 'text-warning',
  critical: 'text-critical',
  relay: 'text-relay',
  direct: 'text-direct',
  info: 'text-info',
  neutral: 'text-fg-secondary',
  accent: 'text-accent-text',
}

export const TONE_SOFT_BG: Record<BadgeTone, string> = {
  online: 'bg-online-soft',
  offline: 'bg-offline-soft',
  warning: 'bg-warning-soft',
  critical: 'bg-critical-soft',
  relay: 'bg-relay-soft',
  direct: 'bg-direct-soft',
  info: 'bg-info-soft',
  neutral: 'bg-surface-inset',
  accent: 'bg-accent-soft',
}

export const TONE_FILL_BG: Record<BadgeTone, string> = {
  online: 'bg-online-fill',
  offline: 'bg-offline-fill',
  warning: 'bg-warning-fill',
  critical: 'bg-critical-fill',
  relay: 'bg-relay-fill',
  direct: 'bg-direct-fill',
  info: 'bg-info-fill',
  neutral: 'bg-fg-muted',
  accent: 'bg-accent',
}

export const TONE_FILL_TEXT: Record<BadgeTone, string> = {
  online: 'text-online-fill',
  offline: 'text-offline-fill',
  warning: 'text-warning-fill',
  critical: 'text-critical-fill',
  relay: 'text-relay-fill',
  direct: 'text-direct-fill',
  info: 'text-info-fill',
  neutral: 'text-fg-muted',
  accent: 'text-accent',
}

export interface BadgeProps extends ComponentProps<'span'> {
  tone?: BadgeTone
  variant?: BadgeVariant
  size?: 'sm' | 'md'
  icon?: LucideIcon
  /** Leading status dot in the tone colour. */
  dot?: boolean
  /** Monospace label (versions, ids). */
  mono?: boolean
}

/** Small labelled pill. Never colour-only: always carries text (and optionally an icon/dot). */
export function Badge({ tone = 'neutral', variant = 'soft', size = 'md', icon: Icon, dot, mono, className, children, ...rest }: BadgeProps) {
  return (
    <span
      className={cn(
        'inline-flex max-w-full items-center gap-1 whitespace-nowrap rounded-md font-medium leading-none',
        size === 'sm' ? 'h-5 px-1.5 text-[11px]' : 'h-6 px-2 text-xs',
        variant === 'soft' && cn(TONE_SOFT_BG[tone], TONE_TEXT[tone]),
        variant === 'outline' && cn('border border-current/25 bg-transparent', TONE_TEXT[tone]),
        variant === 'solid' && cn(TONE_FILL_BG[tone], 'text-white dark:text-fg-inverted'),
        mono && 'font-mono tracking-tight',
        className,
      )}
      {...rest}
    >
      {dot ? <span aria-hidden="true" className={cn('size-1.5 shrink-0 rounded-full', variant === 'solid' ? 'bg-current' : TONE_FILL_BG[tone])} /> : null}
      {Icon ? <Icon className={cn('shrink-0', size === 'sm' ? 'size-3' : 'size-3.5')} aria-hidden="true" /> : null}
      <span className="truncate">{children}</span>
    </span>
  )
}
