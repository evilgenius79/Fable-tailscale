import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { ChevronRight } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { cn } from '../../lib/cn'

export interface Breadcrumb {
  label: ReactNode
  to?: string
}

export interface PageHeaderProps {
  title: ReactNode
  description?: ReactNode
  /** Small uppercase label above the title. */
  eyebrow?: ReactNode
  breadcrumbs?: Breadcrumb[]
  icon?: LucideIcon
  /** Right-aligned actions (buttons, pickers). Wrap below the title on narrow screens. */
  actions?: ReactNode
  /** Inline meta items under the title (badges, key facts). */
  meta?: ReactNode
  /** Extra row (tabs/filters) rendered under the header. */
  children?: ReactNode
  compact?: boolean
  className?: string
}

/** Page title block: breadcrumbs, title (+icon), description, actions, meta row, optional tabs row. */
export function PageHeader({ title, description, eyebrow, breadcrumbs, icon: Icon, actions, meta, children, compact, className }: PageHeaderProps) {
  return (
    <header className={cn('flex flex-col gap-4', compact ? 'mb-4' : 'mb-6', className)}>
      {breadcrumbs?.length ? (
        <nav aria-label="Breadcrumb" className="flex items-center gap-1 text-xs text-fg-muted">
          {breadcrumbs.map((b, i) => (
            <span key={i} className="flex items-center gap-1">
              {i > 0 ? <ChevronRight className="size-3 text-fg-faint" aria-hidden="true" /> : null}
              {b.to ? (
                <Link to={b.to} className="rounded hover:text-fg focus-ring">
                  {b.label}
                </Link>
              ) : (
                <span aria-current={i === breadcrumbs.length - 1 ? 'page' : undefined} className={cn(i === breadcrumbs.length - 1 && 'text-fg-secondary')}>
                  {b.label}
                </span>
              )}
            </span>
          ))}
        </nav>
      ) : null}
      <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div className="flex min-w-0 items-start gap-3">
          {Icon ? (
            <span className="mt-0.5 hidden size-9 shrink-0 items-center justify-center rounded-lg border border-border bg-surface-raised text-fg-secondary shadow-xs sm:flex">
              <Icon className="size-4" aria-hidden="true" />
            </span>
          ) : null}
          <div className="min-w-0">
            {eyebrow ? <p className="mb-1 text-[11px] font-semibold uppercase tracking-wider text-fg-muted">{eyebrow}</p> : null}
            <h1 className={cn('truncate font-semibold tracking-tight text-fg', compact ? 'text-lg leading-7' : 'text-xl leading-8 sm:text-2xl')}>{title}</h1>
            {description ? <p className="mt-1 max-w-2xl text-sm leading-6 text-fg-muted">{description}</p> : null}
            {meta ? <div className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1.5 text-xs text-fg-secondary">{meta}</div> : null}
          </div>
        </div>
        {actions ? <div className="flex shrink-0 flex-wrap items-center gap-2 sm:justify-end">{actions}</div> : null}
      </div>
      {children}
    </header>
  )
}
