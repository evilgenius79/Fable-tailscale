import { useMemo, useState } from 'react'
import { Apple, Container, ExternalLink, KeyRound, Monitor, ShieldCheck, Terminal, type LucideIcon } from 'lucide-react'
import type { HubInfo, Identity, Settings } from '../../api/types'
import { Tabs } from '../ui/Tabs'
import { Tag } from '../ui/Tag'
import { Toggle } from '../ui/Toggle'
import { CodeBlock } from './CodeBlock'
import { SettingsSection } from './SettingsSection'
import { agentInstallSnippets, hubTagFor, type InstallPlatform } from './settings'

const PLATFORM_ICONS: Record<InstallPlatform, LucideIcon> = { linux: Terminal, macos: Apple, windows: Monitor, docker: Container }
const AGENT_DOCS = 'https://github.com/evilgenius79/fable-tailscale/blob/main/docs/AGENT.md'

export interface AgentInstallSectionProps {
  settings: Settings | undefined
  hub: HubInfo | null | undefined
  me: Identity | undefined
}

export function AgentInstallSection({ settings, hub, me }: AgentInstallSectionProps) {
  const [platform, setPlatform] = useState<InstallPlatform>('linux')
  // The API deliberately does not reveal whether TAILWATCH_AGENT_TOKEN is set; the operator flips this locally.
  const [token, setToken] = useState(false)
  const port = settings?.agentPort ?? hub?.agentPort ?? 41820
  const hubTag = hubTagFor(me?.nodeId === hub?.selfId ? me?.tags : undefined)
  const hubName = hub?.selfName
  const snippets = useMemo(() => agentInstallSnippets({ port, tokenConfigured: token, hubTag, hubName }), [port, token, hubTag, hubName])
  const current = snippets.find((s) => s.id === platform) ?? snippets[0]!

  return (
    <SettingsSection id="agent" icon={Terminal} title="Agent install" description="tailwatch-agent is a tiny read-only daemon that serves CPU, memory, disk, network and temperature metrics on the device's Tailscale IP.">
      <div className="space-y-5">
        <div className="grid gap-3 md:grid-cols-2">
          <div className="flex gap-3 rounded-lg border border-border bg-surface-inset p-3">
            <ShieldCheck className="mt-0.5 size-4 shrink-0 text-online" aria-hidden="true" />
            <div className="text-[13px] leading-5 text-fg-secondary">
              <p className="font-medium text-fg">WhoIs authentication</p>
              <p>
                For every request the agent asks its local tailscaled who is behind the source IP. By default it answers nodes owned by the same user and tagged nodes carrying{' '}
                <span className="font-mono text-xs text-fg">tag:tailwatch</span>. Use <span className="font-mono text-xs text-fg">--allow-tag</span> / <span className="font-mono text-xs text-fg">--allow-node</span> to pin it to the hub. There are no write endpoints.
              </p>
            </div>
          </div>
          <div className="flex gap-3 rounded-lg border border-border bg-surface-inset p-3">
            <KeyRound className="mt-0.5 size-4 shrink-0 text-fg-secondary" aria-hidden="true" />
            <div className="text-[13px] leading-5 text-fg-secondary">
              <p className="font-medium text-fg">Optional shared token</p>
              <p>
                Set the same <span className="font-mono text-xs text-fg">TAILWATCH_AGENT_TOKEN</span> on the hub and on every agent, then run agents with <span className="font-mono text-xs text-fg">--auth both</span> for defence in depth on shared tailnets. Tokens live in environment files only, never on the command line.
              </p>
            </div>
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-x-6 gap-y-3 rounded-lg border border-border p-3">
          <div className="text-xs text-fg-muted">
            <span className="font-semibold uppercase tracking-wider text-fg-muted">Hub</span>{' '}
            {hubName ? <span className="font-medium text-fg">{hubName}</span> : '—'}
            {hubTag ? (
              <>
                {' '}
                · <Tag tag={hubTag} size="sm" />
              </>
            ) : (
              <span> · untagged</span>
            )}
            <span>
              {' '}
              · port <span className="num font-medium text-fg">{port}</span>
            </span>
          </div>
          <Toggle size="sm" checked={token} onCheckedChange={setToken} label="Hub uses a shared token" labelPosition="end" className="items-center" />
        </div>

        <Tabs<InstallPlatform>
          aria-label="Install platform"
          variant="pills"
          size="sm"
          idPrefix="agent-install"
          value={platform}
          onValueChange={setPlatform}
          tabs={snippets.map((s) => ({ id: s.id, label: s.label, icon: PLATFORM_ICONS[s.id] }))}
        />
        <div role="tabpanel" id={`agent-install-${current.id}-panel`} aria-labelledby={`agent-install-${current.id}`} className="space-y-4">
          <p className="text-[13px] leading-5 text-fg-secondary">{current.intro}</p>
          {current.steps.map((step, i) => (
            <div key={step.title} className="space-y-1.5">
              <p className="flex items-center gap-2 text-[13px] font-medium text-fg">
                <span className="num flex size-5 items-center justify-center rounded-full bg-surface-inset text-[11px] font-semibold text-fg-secondary" aria-hidden="true">
                  {i + 1}
                </span>
                {step.title}
              </p>
              <CodeBlock code={step.code} label={`Copy: ${step.title}`} />
              {step.note ? <p className="text-xs text-fg-muted">{step.note}</p> : null}
            </div>
          ))}
        </div>
        <p className="text-xs text-fg-muted">
          Flags, allow-lists and troubleshooting:{' '}
          <a href={AGENT_DOCS} target="_blank" rel="noopener noreferrer" className="inline-flex items-center gap-1 rounded font-medium text-accent-text hover:underline focus-ring">
            docs/AGENT.md
            <ExternalLink className="size-3" aria-hidden="true" />
          </a>
          . The hub must be allowed to reach TCP {port} on each device in your Tailscale ACL.
        </p>
      </div>
    </SettingsSection>
  )
}
