import type { ReactNode } from 'react'
import { AlertTriangle, RefreshCw, ShieldOff, WifiOff } from 'lucide-react'
import { cn } from '../../lib/cn'
import { errorMessage, errorTitle, isApiError } from '../../api/client'
import { Button } from './Button'

export interface ErrorStateProps {
  error?: unknown
  title?: ReactNode
  description?: ReactNode
  onRetry?: () => void
  retrying?: boolean
  /** Inline banner instead of a centred block. */
  compact?: boolean
  className?: string
}

/** Renders an ApiError (or anything thrown) with a retry affordance. */
export function ErrorState({ error, title, description, onRetry, retrying, compact, className }: ErrorStateProps) {
  const heading = title ?? errorTitle(error)
  const detail = description ?? errorMessage(error)
  const Icon = isApiError(error) && error.isNetwork ? WifiOff : isApiError(error) && error.isAuth ? ShieldOff : AlertTriangle

  if (compact) {
    return (
      <div role="alert" className={cn('flex items-start gap-3 rounded-lg border border-critical/30 bg-critical-soft px-3 py-2.5 text-sm', className)}>
        <Icon className="mt-0.5 size-4 shrink-0 text-critical" aria-hidden="true" />
        <div className="min-w-0 flex-1">
          <p className="font-medium text-fg">{heading}</p>
          {detail ? <p className="text-xs text-fg-secondary break-words">{detail}</p> : null}
        </div>
        {onRetry ? (
          <Button size="xs" variant="outline" onClick={onRetry} loading={retrying} leadingIcon={RefreshCw}>
            Retry
          </Button>
        ) : null}
      </div>
    )
  }

  return (
    <div role="alert" className={cn('flex flex-col items-center justify-center gap-3 rounded-lg border border-border bg-surface px-6 py-14 text-center', className)}>
      <span className="flex size-12 items-center justify-center rounded-xl bg-critical-soft text-critical">
        <Icon className="size-5" aria-hidden="true" />
      </span>
      <div className="max-w-sm">
        <p className="text-base font-semibold text-fg">{heading}</p>
        {detail ? <p className="mt-1 text-sm leading-6 text-fg-muted break-words">{detail}</p> : null}
        {isApiError(error) && error.status ? (
          <p className="mt-2 font-mono text-[11px] text-fg-muted">
            {error.code} · HTTP {error.status}
          </p>
        ) : null}
      </div>
      {onRetry ? (
        <Button variant="secondary" onClick={onRetry} loading={retrying} leadingIcon={RefreshCw}>
          Try again
        </Button>
      ) : null}
    </div>
  )
}
