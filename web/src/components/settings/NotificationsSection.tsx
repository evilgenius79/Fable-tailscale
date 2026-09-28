import { useState } from 'react'
import { BellRing, MessageSquare, Send, Webhook, type LucideIcon } from 'lucide-react'
import type { Settings } from '../../api/types'
import { TestNotificationDialog } from '../alerts/TestNotificationDialog'
import { Badge } from '../ui/Badge'
import { Button } from '../ui/Button'
import { EmptyState } from '../ui/EmptyState'
import { SkeletonText } from '../ui/Skeleton'
import { Tooltip } from '../ui/Tooltip'
import { SettingsSection } from './SettingsSection'
import { notifierMeta } from './settings'

const ICONS: Record<string, LucideIcon> = { webhook: Webhook, slack: MessageSquare, ntfy: BellRing }

export interface NotificationsSectionProps {
  settings: Settings | undefined
  loading: boolean
  isAdmin: boolean
}

export function NotificationsSection({ settings, loading, isAdmin }: NotificationsSectionProps) {
  const [testOpen, setTestOpen] = useState(false)
  const notifiers = settings?.notifiers ?? []
  const testButton = (
    <Button size="sm" leadingIcon={Send} onClick={() => setTestOpen(true)} disabled={!isAdmin || !settings}>
      Send test
    </Button>
  )
  return (
    <SettingsSection
      id="notifications"
      icon={BellRing}
      title="Notifications"
      description="Where alerts go when a rule with Notify enabled opens or resolves. Targets are configured on the hub; the API never exposes URLs or tokens."
      actions={
        isAdmin ? (
          testButton
        ) : (
          <Tooltip content="Only admins can send test notifications" side="left">
            <span className="inline-flex" tabIndex={0}>
              {testButton}
            </span>
          </Tooltip>
        )
      }
    >
      {loading && !settings ? (
        <SkeletonText lines={3} />
      ) : notifiers.length ? (
        <ul className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {notifiers.map((kind) => {
            const m = notifierMeta(kind)
            const Icon = ICONS[m.kind] ?? Webhook
            return (
              <li key={kind} className="flex items-start gap-3 rounded-lg border border-border p-3">
                <span className="flex size-8 shrink-0 items-center justify-center rounded-md bg-surface-inset text-fg-secondary">
                  <Icon className="size-4" aria-hidden="true" />
                </span>
                <div className="min-w-0">
                  <p className="flex items-center gap-2 text-[13px] font-semibold text-fg">
                    {m.label}
                    <Badge size="sm" tone="online" dot>
                      configured
                    </Badge>
                  </p>
                  <p className="mt-0.5 text-xs leading-5 text-fg-muted">{m.description}</p>
                </div>
              </li>
            )
          })}
        </ul>
      ) : (
        <EmptyState
          size="sm"
          icon={BellRing}
          title="No notifiers configured"
          description="Set TAILWATCH_WEBHOOK_URL, TAILWATCH_SLACK_WEBHOOK_URL or TAILWATCH_NTFY_URL on the hub and restart it. Alerts still show up here and in the event log."
        />
      )}
      <TestNotificationDialog open={testOpen} onClose={() => setTestOpen(false)} notifiers={notifiers} />
    </SettingsSection>
  )
}
