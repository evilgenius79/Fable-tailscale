import type { ReactNode } from 'react'
import type { LucideIcon } from 'lucide-react'
import { Inbox } from 'lucide-react'
import { cn } from '../../lib/cn'

export interface EmptyStateProps {
  icon?: LucideIcon
  title: ReactNode
  description?: ReactNode
  action?: ReactNode
  size?: 'sm' | 'md' | 'lg'
  /** Draw the dashed container (default true). */
  bordered?: boolean
  className?: string
}

/** Calm empty state: icon in a soft tile, title, optional description and action. */
export function EmptyState({ icon: Icon = Inbox, title, description, action, size = 'md', bordered = true, className }: EmptyStateProps) {
  return (
    <div
      className={cn(
        'flex flex-col items-center justify-center text-center',
        bordered && 'rounded-lg border border-dashed border-border-strong bg-surface/40',
        size === 'sm' ? 'gap-2 px-4 py-8' : size === 'lg' ? 'gap-3 px-6 py-20' : 'gap-3 px-6 py-14',
        className,
      )}
    >
      <span
        className={cn(
          'flex items-center justify-center rounded-xl border border-border bg-surface-raised text-fg-muted shadow-xs',
          size === 'sm' ? 'size-9' : 'size-12',
        )}
      >
        <Icon className={size === 'sm' ? 'size-4' : 'size-5'} aria-hidden="true" />
      </span>
      <div className="max-w-sm">
        <p className={cn('font-semibold text-fg', size === 'sm' ? 'text-sm' : 'text-base')}>{title}</p>
        {description ? <p className="mt-1 text-sm leading-6 text-fg-muted text-balance">{description}</p> : null}
      </div>
      {action ? <div className="mt-1 flex items-center gap-2">{action}</div> : null}
    </div>
  )
}
