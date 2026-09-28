import type { ReactNode } from 'react'
import { Cloud, Database, Radio, Timer, type LucideIcon } from 'lucide-react'
import type { HubInfo, Settings } from '../../api/types'
import { cn } from '../../lib/cn'
import { formatDateTime, formatRelative } from '../../lib/format'
import type { StatusTone } from '../../lib/status'
import { Badge } from '../ui/Badge'
import { SkeletonText } from '../ui/Skeleton'
import { SettingsSection } from './SettingsSection'
import { formatIntervalSeconds } from './settings'

function SourceCard({ icon: Icon, title, tone, status, rows, note }: { icon: LucideIcon; title: string; tone: StatusTone; status: string; rows: { k: string; v: ReactNode; mono?: boolean }[]; note?: string }) {
  return (
    <div className="flex min-w-0 flex-col rounded-lg border border-border p-3 sm:p-4">
      <div className="flex items-center gap-2.5">
        <span className="flex size-7 items-center justify-center rounded-md bg-surface-inset text-fg-secondary">
          <Icon className="size-4" aria-hidden="true" />
        </span>
        <p className="min-w-0 flex-1 truncate text-[13px] font-semibold text-fg">{title}</p>
        <Badge size="sm" tone={tone} dot>
          {status}
        </Badge>
      </div>
      <dl className="mt-3 space-y-1.5">
        {rows.map((r) => (
          <div key={r.k} className="flex items-baseline justify-between gap-3 text-[13px]">
            <dt className="shrink-0 text-fg-muted">{r.k}</dt>
            <dd className={cn('min-w-0 truncate text-right text-fg', r.mono && 'font-mono text-xs')}>{r.v}</dd>
          </div>
        ))}
      </dl>
      {note ? <p className="mt-3 text-[11px] leading-4 text-fg-muted">{note}</p> : null}
    </div>
  )
}

function Interval({ label, value, hint }: { label: string; value: string; hint: string }) {
  return (
    <div className="rounded-lg border border-border px-3 py-2.5">
      <p className="text-[11px] font-semibold uppercase tracking-wider text-fg-muted">{label}</p>
      <p className="num mt-0.5 text-lg font-semibold leading-7 text-fg">{value}</p>
      <p className="text-[11px] text-fg-muted">{hint}</p>
    </div>
  )
}

export interface DataSourcesSectionProps {
  settings: Settings | undefined
  hub: HubInfo | null | undefined
  loading: boolean
}

export function DataSourcesSection({ settings, hub, loading }: DataSourcesSectionProps) {
  if (loading && !settings) {
    return (
      <SettingsSection id="sources" icon={Database} title="Data sources">
        <SkeletonText lines={5} />
      </SettingsSection>
    )
  }
  const control = settings?.controlApiEnabled ?? hub?.controlApiEnabled ?? false
  const agents = settings?.agentEnabled ?? true
  return (
    <SettingsSection id="sources" icon={Database} title="Data sources" description="Where the hub gets its facts: the local tailscaled, the optional Tailscale control API and per-device agents.">
      <div className="grid gap-3 md:grid-cols-3">
        <SourceCard
          icon={Radio}
          title="Local tailscaled"
          tone="online"
          status="Always on"
          rows={[
            { k: 'Version', v: hub ? `v${hub.tailscaleVersion}` : '—', mono: true },
            { k: 'Poll interval', v: formatIntervalSeconds(settings?.pollIntervalSeconds ?? hub?.pollIntervalSeconds) },
            { k: 'Last poll', v: hub ? <span title={formatDateTime(hub.lastPoll)}>{formatRelative(hub.lastPoll)}</span> : '—' },
          ]}
          note="Peer status, direct/relay path, latency pings and WireGuard byte counters come from the LocalAPI."
        />
        <SourceCard
          icon={Cloud}
          title="Control API"
          tone={control ? 'online' : 'neutral'}
          status={control ? 'Configured' : 'Not configured'}
          rows={[
            { k: 'Tailnet', v: settings?.tailnet || (control ? 'default' : '—') },
            { k: 'Interval', v: control ? formatIntervalSeconds(settings?.apiIntervalSeconds) : '—' },
            { k: 'Last poll', v: hub?.lastApiPoll ? <span title={formatDateTime(hub.lastApiPoll)}>{formatRelative(hub.lastApiPoll)}</span> : '—' },
            {
              k: 'Admin actions',
              v: (
                <Badge size="sm" tone={settings?.adminActionsEnabled ? 'online' : 'neutral'}>
                  {settings?.adminActionsEnabled ? 'enabled' : 'disabled'}
                </Badge>
              ),
            },
          ]}
          note={control ? 'Adds client versions, authorization state, key expiry, routes and NAT traits. Credentials never leave the hub.' : 'Set TS_OAUTH_CLIENT_ID / TS_OAUTH_CLIENT_SECRET (or TS_API_KEY) on the hub to enrich the inventory and enable admin actions.'}
        />
        <SourceCard
          icon={Timer}
          title="Agent collection"
          tone={agents ? 'online' : 'neutral'}
          status={agents ? 'Enabled' : 'Disabled'}
          rows={[
            { k: 'Port', v: settings?.agentPort ?? hub?.agentPort ?? '—', mono: true },
            { k: 'Auth', v: 'WhoIs · optional token' },
            { k: 'Shared token', v: 'never exposed', mono: true },
            { k: 'Timeout', v: '5s per device' },
          ]}
          note="The hub calls GET /v1/metrics on each device's Tailscale IP. Whether TAILWATCH_AGENT_TOKEN is set is not reported by the API by design."
        />
      </div>
      <div className="mt-4 grid grid-cols-3 gap-3">
        <Interval label="Poll" value={formatIntervalSeconds(settings?.pollIntervalSeconds ?? hub?.pollIntervalSeconds)} hint="LocalAPI + agents" />
        <Interval label="API" value={control ? formatIntervalSeconds(settings?.apiIntervalSeconds) : 'off'} hint="control API" />
        <Interval label="Ping" value={formatIntervalSeconds(settings?.pingIntervalSeconds)} hint="disco latency" />
      </div>
    </SettingsSection>
  )
}
