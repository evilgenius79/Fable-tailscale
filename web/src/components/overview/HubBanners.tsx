import { useState, type ReactNode } from 'react'
import { AlertTriangle, ShieldAlert, X } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import type { HubInfo } from '../../api/types'
import { cn } from '../../lib/cn'
import { formatRelative } from '../../lib/format'

interface BannerProps {
  tone: 'warning' | 'critical'
  icon: LucideIcon
  title: ReactNode
  children?: ReactNode
  onDismiss: () => void
}

function Banner({ tone, icon: Icon, title, children, onDismiss }: BannerProps) {
  return (
    <div
      role={tone === 'critical' ? 'alert' : 'status'}
      className={cn(
        'flex items-start gap-3 rounded-lg border px-3.5 py-3 text-sm shadow-xs animate-fade-in',
        tone === 'critical' ? 'border-critical/30 bg-critical-soft' : 'border-warning/30 bg-warning-soft',
      )}
    >
      <Icon className={cn('mt-0.5 size-4 shrink-0', tone === 'critical' ? 'text-critical' : 'text-warning')} aria-hidden="true" />
      <div className="min-w-0 flex-1">
        <p className="font-medium text-fg">{title}</p>
        {children}
      </div>
      <button type="button" onClick={onDismiss} aria-label="Dismiss" className="-m-1 rounded-md p-1 text-fg-muted hover:bg-surface/60 hover:text-fg focus-ring">
        <X className="size-4" aria-hidden="true" />
      </button>
    </div>
  )
}

/**
 * tailscaled health warnings and the hub's last poll error, each dismissible.
 * A dismissal is keyed to the message text, so a new or changed warning shows again.
 */
export function HubBanners({ hub }: { hub: HubInfo | null | undefined }) {
  const health = hub?.health ?? []
  const healthKey = health.join('\n')
  const lastError = hub?.lastError ?? ''
  const [dismissedHealth, setDismissedHealth] = useState<string | null>(null)
  const [dismissedError, setDismissedError] = useState<string | null>(null)
  const showHealth = health.length > 0 && dismissedHealth !== healthKey
  const showError = lastError.length > 0 && dismissedError !== lastError
  if (!showHealth && !showError) return null
  return (
    <div className="mb-5 space-y-3">
      {showError ? (
        <Banner tone="critical" icon={ShieldAlert} title="Hub error" onDismiss={() => setDismissedError(lastError)}>
          <p className="mt-0.5 break-words text-[13px] text-fg-secondary">{lastError}</p>
          {hub?.lastPoll ? <p className="mt-1 text-xs text-fg-muted">Last successful poll {formatRelative(hub.lastPoll)}</p> : null}
        </Banner>
      ) : null}
      {showHealth ? (
        <Banner tone="warning" icon={AlertTriangle} title={health.length === 1 ? 'tailscaled reports a health warning' : `tailscaled reports ${health.length} health warnings`} onDismiss={() => setDismissedHealth(healthKey)}>
          <ul className="mt-1 list-disc space-y-0.5 pl-4 text-[13px] text-fg-secondary">
            {health.map((h, i) => (
              <li key={i} className="break-words">
                {h}
              </li>
            ))}
          </ul>
        </Banner>
      ) : null}
    </div>
  )
}
