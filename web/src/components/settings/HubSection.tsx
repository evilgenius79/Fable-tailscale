import { Link } from 'react-router-dom'
import { Server } from 'lucide-react'
import type { HubInfo, Settings } from '../../api/types'
import { formatDateTime, formatDuration, formatRelative } from '../../lib/format'
import { Badge } from '../ui/Badge'
import { KeyValueList } from '../ui/KeyValue'
import { SkeletonText } from '../ui/Skeleton'
import { SettingsSection } from './SettingsSection'
import { backendStateTone, hubHealthLabel, hubHealthTone } from './settings'

export interface HubSectionProps {
  hub: HubInfo | null | undefined
  settings: Settings | undefined
  loading: boolean
}

export function HubSection({ hub, settings, loading }: HubSectionProps) {
  const started = hub?.startedAt ? new Date(hub.startedAt).getTime() : null
  const uptime = started ? Math.max(0, Math.round((Date.now() - started) / 1000)) : null
  return (
    <SettingsSection id="hub" icon={Server} title="Hub" description="The Tailwatch process, its tailnet node and the local tailscaled it talks to.">
      {loading && !hub ? (
        <SkeletonText lines={6} />
      ) : (
        <>
          <KeyValueList
            columns={2}
            items={[
              { key: 'Version', value: hub?.version ?? '—', mono: true, copy: hub?.version },
              { key: 'Tailnet', value: hub?.tailnet ?? settings?.tailnet ?? '—' },
              {
                key: 'Self node',
                value: hub ? (
                  <Link to={`/devices/${encodeURIComponent(hub.selfId)}`} className="rounded font-medium text-accent-text hover:underline focus-ring">
                    {hub.selfName}
                  </Link>
                ) : (
                  '—'
                ),
                hint: hub?.selfId,
              },
              { key: 'MagicDNS suffix', value: hub?.magicDnsSuffix ?? '—', mono: true },
              { key: 'Tailscale IPs', value: hub?.selfIps.join(', ') ?? '—', mono: true, copy: hub?.selfIps[0] },
              { key: 'tailscaled', value: hub ? `v${hub.tailscaleVersion}` : '—', mono: true },
              {
                key: 'Backend state',
                value: hub ? (
                  <Badge size="sm" tone={backendStateTone(hub.backendState)} dot>
                    {hub.backendState}
                  </Badge>
                ) : (
                  '—'
                ),
              },
              {
                key: 'Health',
                value: (
                  <Badge size="sm" tone={hubHealthTone(hub)} dot>
                    {hubHealthLabel(hub)}
                  </Badge>
                ),
              },
              { key: 'Started', value: hub ? `${formatRelative(hub.startedAt)}${uptime ? ` · up ${formatDuration(uptime, 2)}` : ''}` : '—', hint: hub ? formatDateTime(hub.startedAt) : undefined },
              { key: 'Listen', value: settings?.listen ?? '—', mono: true, copy: settings?.listen },
              { key: 'Last poll', value: hub ? formatRelative(hub.lastPoll) : '—', hint: hub ? formatDateTime(hub.lastPoll) : undefined },
              { key: 'Last API poll', value: hub?.lastApiPoll ? formatRelative(hub.lastApiPoll) : hub?.controlApiEnabled ? 'never' : 'n/a', hint: hub?.lastApiPoll ? formatDateTime(hub.lastApiPoll) : undefined },
            ]}
          />
          {hub?.health.length ? (
            <div className="mt-4 rounded-lg border border-warning/30 bg-warning-soft px-3 py-2.5 text-[13px]">
              <p className="font-medium text-fg">tailscaled health warnings</p>
              <ul className="mt-1 list-disc space-y-0.5 pl-4 text-fg-secondary">
                {hub.health.map((h, i) => (
                  <li key={i} className="break-words">
                    {h}
                  </li>
                ))}
              </ul>
            </div>
          ) : null}
          {hub?.lastError ? (
            <div className="mt-4 rounded-lg border border-critical/30 bg-critical-soft px-3 py-2.5 text-[13px]">
              <p className="font-medium text-fg">Last poll error</p>
              <p className="mt-0.5 break-words font-mono text-xs text-fg-secondary">{hub.lastError}</p>
            </div>
          ) : null}
        </>
      )}
    </SettingsSection>
  )
}
