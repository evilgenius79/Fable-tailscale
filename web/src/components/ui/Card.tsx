import type { ComponentProps, ElementType, ReactNode } from 'react'
import type { LucideIcon } from 'lucide-react'
import { cn } from '../../lib/cn'

export type CardPadding = 'none' | 'sm' | 'md' | 'lg'

const PAD: Record<CardPadding, string> = { none: '', sm: 'p-3', md: 'p-4 sm:p-5', lg: 'p-5 sm:p-6' }

export interface CardProps extends ComponentProps<'div'> {
  as?: ElementType
  padding?: CardPadding
  /** Hover/focus affordance for clickable cards. */
  interactive?: boolean
  /** Raised surface + stronger shadow (dialogs, popovers, KPI tiles). */
  raised?: boolean
}

/** Layered surface: 1px border, subtle shadow, 12px radius. */
export function Card({ as, padding = 'md', interactive, raised, className, children, ...rest }: CardProps) {
  const Comp = (as ?? 'div') as ElementType
  return (
    <Comp
      className={cn(
        'rounded-lg border border-border',
        raised ? 'bg-surface-raised shadow-md' : 'bg-surface shadow-xs',
        interactive && 'transition-colors hover:border-border-strong hover:bg-surface-hover focus-ring cursor-pointer',
        PAD[padding],
        className,
      )}
      {...rest}
    >
      {children}
    </Comp>
  )
}

export interface CardHeaderProps {
  title: ReactNode
  description?: ReactNode
  icon?: LucideIcon
  actions?: ReactNode
  className?: string
  /** Draw a hairline below the header (use when the body is flush/no-padding). */
  divider?: boolean
  children?: ReactNode
}

export function CardHeader({ title, description, icon: Icon, actions, className, divider, children }: CardHeaderProps) {
  return (
    <div className={cn('flex flex-col gap-3', divider && 'border-b border-border px-4 py-3 sm:px-5', !divider && 'mb-4', className)}>
      <div className="flex items-start justify-between gap-3">
        <div className="flex min-w-0 items-start gap-2.5">
          {Icon ? (
            <span className="mt-0.5 flex size-6 shrink-0 items-center justify-center rounded-md bg-surface-inset text-fg-secondary">
              <Icon className="size-3.5" aria-hidden="true" />
            </span>
          ) : null}
          <div className="min-w-0">
            <h3 className="text-sm font-semibold leading-6 text-fg">{title}</h3>
            {description ? <p className="text-xs leading-5 text-fg-muted">{description}</p> : null}
          </div>
        </div>
        {actions ? <div className="flex shrink-0 items-center gap-1.5">{actions}</div> : null}
      </div>
      {children}
    </div>
  )
}

export function CardContent({ className, ...rest }: ComponentProps<'div'>) {
  return <div className={cn('min-w-0', className)} {...rest} />
}

export function CardFooter({ className, ...rest }: ComponentProps<'div'>) {
  return <div className={cn('mt-4 flex items-center justify-between gap-3 border-t border-border pt-3 text-xs text-fg-muted', className)} {...rest} />
}
