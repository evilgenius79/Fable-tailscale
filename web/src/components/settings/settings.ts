// Pure helpers for the Settings page: store breakdown for the stacked bar,
// retention labels, notifier metadata, audit action labels and the agent
// install snippets. No React; unit-tested in settings.test.ts.

import type { AuditEntry, HubInfo, Settings, StoreStats } from '../../api/types'
import { formatDuration } from '../../lib/format'
import type { StatusTone } from '../../lib/status'

// ---------------------------------------------------------------------------
// Storage
// ---------------------------------------------------------------------------

export interface StoreSegment {
  key: 'samples' | 'rollups' | 'events' | 'alerts'
  label: string
  value: number
}

/** Row counts by table, in fixed order (categorical slots 1–4). */
export function storeSegments(store: StoreStats | undefined): StoreSegment[] {
  return [
    { key: 'samples', label: 'Raw samples', value: store?.samples ?? 0 },
    { key: 'rollups', label: 'Rollups', value: store?.rollups ?? 0 },
    { key: 'events', label: 'Events', value: store?.events ?? 0 },
    { key: 'alerts', label: 'Alerts', value: store?.alerts ?? 0 },
  ]
}

export function totalRows(store: StoreStats | undefined): number {
  return storeSegments(store).reduce((a, s) => a + s.value, 0)
}

/** "48 hours" → "2 days", 720 → "30 days", 36 → "1 day 12 hours". */
export function formatRetentionHours(hours: number | null | undefined): string {
  if (hours === null || hours === undefined || !Number.isFinite(hours) || hours <= 0) return '—'
  if (hours % 24 === 0) {
    const d = hours / 24
    return `${d} ${d === 1 ? 'day' : 'days'}`
  }
  return formatDuration(hours * 3600, 2)
}

export function formatRetentionDays(days: number | null | undefined): string {
  if (days === null || days === undefined || !Number.isFinite(days) || days <= 0) return '—'
  return `${days} ${days === 1 ? 'day' : 'days'}`
}

/** Interval in seconds → "15s", "1m", "5m 30s"; "off" for 0 (pings can be disabled). */
export function formatIntervalSeconds(seconds: number | null | undefined): string {
  if (seconds === null || seconds === undefined || !Number.isFinite(seconds)) return '—'
  if (seconds <= 0) return 'off'
  return formatDuration(seconds, 2)
}

// ---------------------------------------------------------------------------
// Hub / auth
// ---------------------------------------------------------------------------

export function authModeLabel(mode: string | undefined): string {
  if (mode === 'tailscale') return 'Tailscale identity (WhoIs)'
  if (mode === 'none') return 'None (open)'
  return mode || '—'
}

export function backendStateTone(state: string | undefined): StatusTone {
  switch ((state ?? '').toLowerCase()) {
    case 'running':
      return 'online'
    case 'starting':
    case 'needslogin':
    case 'needsmachineauth':
      return 'warning'
    case 'stopped':
    case 'nostate':
      return 'offline'
    default:
      return 'neutral'
  }
}

export function hubHealthTone(hub: Pick<HubInfo, 'health' | 'lastError'> | null | undefined): StatusTone {
  if (!hub) return 'neutral'
  if (hub.lastError) return 'critical'
  if (hub.health.length) return 'warning'
  return 'online'
}

export function hubHealthLabel(hub: Pick<HubInfo, 'health' | 'lastError'> | null | undefined): string {
  if (!hub) return 'Unknown'
  if (hub.lastError) return 'Poll error'
  if (hub.health.length) return `${hub.health.length} warning${hub.health.length === 1 ? '' : 's'}`
  return 'Healthy'
}

/** "*" means every tailnet identity; otherwise the list, or "nobody". */
export function principalsLabel(list: ReadonlyArray<string> | undefined): { kind: 'everyone' | 'nobody' | 'list'; items: string[] } {
  const items = (list ?? []).map((s) => s.trim()).filter(Boolean)
  if (items.includes('*')) return { kind: 'everyone', items: [] }
  if (!items.length) return { kind: 'nobody', items: [] }
  return { kind: 'list', items }
}

// ---------------------------------------------------------------------------
// Notifications
// ---------------------------------------------------------------------------

export interface NotifierMeta {
  kind: string
  label: string
  description: string
}

const NOTIFIERS: Record<string, Omit<NotifierMeta, 'kind'>> = {
  webhook: { label: 'Webhook', description: 'Generic JSON POST with an optional HMAC signature (X-Tailwatch-Signature).' },
  slack: { label: 'Slack / Discord', description: 'Incoming-webhook compatible text payload.' },
  ntfy: { label: 'ntfy', description: 'Push notification to an ntfy topic, optionally with a bearer token.' },
}

export function notifierMeta(kind: string): NotifierMeta {
  const k = kind.trim().toLowerCase()
  const m = NOTIFIERS[k]
  return m ? { kind: k, ...m } : { kind: k, label: kind, description: 'Configured notifier.' }
}

// ---------------------------------------------------------------------------
// Audit
// ---------------------------------------------------------------------------

const ACTION_LABELS: Record<string, string> = {
  'device.authorize': 'Authorize device',
  'device.tags': 'Set tags',
  'device.key-expiry': 'Key expiry',
  'device.routes': 'Set routes',
  'device.name': 'Rename device',
  'device.delete': 'Delete device',
  'device.ping': 'Ping device',
  'alert.ack': 'Acknowledge alert',
  'rule.save': 'Save rule',
  'alerts.test': 'Test notification',
  refresh: 'Refresh',
}

/** Human label for an audit action ("device.tags" → "Set tags"); unknown actions are title-cased. */
export function auditActionLabel(action: string): string {
  const known = ACTION_LABELS[action]
  if (known) return known
  return action
    .split(/[._-]+/)
    .filter(Boolean)
    .map((w, i) => (i === 0 ? w.charAt(0).toUpperCase() + w.slice(1) : w))
    .join(' ')
}

/** Where an audit target links to (device ids → device page; alert/rule targets → their tabs). */
export function auditTargetLink(entry: Pick<AuditEntry, 'action' | 'target'>): string | undefined {
  if (!entry.target) return undefined
  if (entry.action.startsWith('device.')) return `/devices/${encodeURIComponent(entry.target)}`
  if (entry.action === 'rule.save') return `/alerts?tab=rules&rule=${encodeURIComponent(entry.target)}`
  if (entry.action === 'alert.ack') return '/alerts?tab=all'
  return undefined
}

/** Stable, pretty JSON for the details disclosure. */
export function formatDetails(details: Record<string, unknown> | undefined): string {
  if (!details || !Object.keys(details).length) return ''
  try {
    return JSON.stringify(details, null, 2)
  } catch {
    return String(details)
  }
}

// ---------------------------------------------------------------------------
// Agent install
// ---------------------------------------------------------------------------

export type InstallPlatform = 'linux' | 'macos' | 'windows' | 'docker'

export interface InstallStep {
  title: string
  code: string
  /** Optional caption under the code block. */
  note?: string
}

export interface InstallSnippet {
  id: InstallPlatform
  label: string
  intro: string
  steps: InstallStep[]
}

export interface AgentInstallOptions {
  /** Hub agent port (Settings.agentPort). */
  port: number
  /** Whether the hub has TAILWATCH_AGENT_TOKEN configured (never the value). */
  tokenConfigured: boolean
  /** Tag the hub node carries, if any (drives --allow-tag). */
  hubTag?: string
  /** MagicDNS name of the hub node (drives --allow-node when untagged). */
  hubName?: string
}

const REPO_RAW = 'https://raw.githubusercontent.com/evilgenius79/fable-tailscale/main'
const RELEASES = 'https://github.com/evilgenius79/fable-tailscale/releases/latest/download'

/** Flags every install variant should pass, derived from the hub configuration. */
export function agentFlags(o: AgentInstallOptions): string[] {
  const flags: string[] = []
  if (o.hubTag) flags.push(`--allow-tag ${o.hubTag}`)
  else if (o.hubName) flags.push(`--allow-node ${o.hubName}`)
  if (o.tokenConfigured) flags.push('--auth both')
  if (o.port !== 41820) flags.push(`--port ${o.port}`)
  return flags
}

/** Copyable install commands per platform, referencing deploy/ and scripts/ in the repo. */
export function agentInstallSnippets(o: AgentInstallOptions): InstallSnippet[] {
  const flags = agentFlags(o)
  const flagStr = flags.length ? ' ' + flags.join(' ') : ''
  const envToken = o.tokenConfigured ? 'sudo TAILWATCH_AGENT_TOKEN="$(cat /path/to/token)" ' : 'sudo '
  const envLines = [
    `TAILWATCH_AGENT_OPTS="${flags.join(' ')}"`,
    ...(o.tokenConfigured ? ['TAILWATCH_AGENT_TOKEN=<same token as the hub>'] : []),
  ].join('\n')
  const portNote = o.port !== 41820 ? ` The hub polls port ${o.port}, so the agent must listen there too.` : ''

  return [
    {
      id: 'linux',
      label: 'Linux (systemd)',
      intro: 'The installer downloads the release binary, creates an unprivileged user, writes /etc/tailwatch/agent.env and enables the hardened unit from deploy/systemd/tailwatch-agent.service.',
      steps: [
        {
          title: 'Download, read, run the installer',
          code: [`curl -fsSLO ${REPO_RAW}/scripts/install-agent.sh`, 'less install-agent.sh', `${envToken}sh install-agent.sh${flagStr}`].join('\n'),
          note: 'Add --yes for unattended runs; --dry-run prints the plan without root.' + portNote,
        },
        {
          title: 'Or install by hand',
          code: [
            `curl -fsSLO ${RELEASES}/tailwatch-agent_linux_amd64`,
            'sudo install -m 0755 tailwatch-agent_linux_amd64 /usr/local/bin/tailwatch-agent',
            'sudo useradd --system --no-create-home --shell /usr/sbin/nologin tailwatch-agent',
            'sudo install -d -m 0755 /etc/tailwatch',
            "sudo tee /etc/tailwatch/agent.env >/dev/null <<'EOF'",
            envLines,
            'EOF',
            'sudo chmod 0600 /etc/tailwatch/agent.env',
            'sudo cp deploy/systemd/tailwatch-agent.service /etc/systemd/system/',
            'sudo systemctl daemon-reload && sudo systemctl enable --now tailwatch-agent',
          ].join('\n'),
          note: 'Logs: journalctl -u tailwatch-agent -f',
        },
      ],
    },
    {
      id: 'macos',
      label: 'macOS (launchd)',
      intro: 'Use the open-source tailscaled (brew install tailscale) rather than the App Store app so the agent can reach the LocalAPI socket for WhoIs.',
      steps: [
        {
          title: 'Install the binary',
          code: [
            `curl -fsSLO ${RELEASES}/tailwatch-agent_darwin_arm64`,
            'sudo install -m 0755 tailwatch-agent_darwin_arm64 /usr/local/bin/tailwatch-agent',
          ].join('\n'),
          note: 'Use tailwatch-agent_darwin_amd64 on Intel Macs.',
        },
        {
          title: 'Create a LaunchDaemon',
          code: [
            'sudo tee /Library/LaunchDaemons/dev.tailwatch.agent.plist >/dev/null <<\'EOF\'',
            '<?xml version="1.0" encoding="UTF-8"?>',
            '<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">',
            '<plist version="1.0"><dict>',
            '  <key>Label</key><string>dev.tailwatch.agent</string>',
            '  <key>ProgramArguments</key><array>',
            '    <string>/usr/local/bin/tailwatch-agent</string>',
            ...flags.flatMap((f) => f.split(' ').map((part) => `    <string>${part}</string>`)),
            '  </array>',
            ...(o.tokenConfigured
              ? ['  <key>EnvironmentVariables</key><dict>', '    <key>TAILWATCH_AGENT_TOKEN</key><string>SAME-TOKEN-AS-THE-HUB</string>', '  </dict>']
              : []),
            '  <key>RunAtLoad</key><true/>',
            '  <key>KeepAlive</key><true/>',
            '</dict></plist>',
            'EOF',
            'sudo chmod 0600 /Library/LaunchDaemons/dev.tailwatch.agent.plist',
            'sudo launchctl bootstrap system /Library/LaunchDaemons/dev.tailwatch.agent.plist',
          ].join('\n'),
          note: 'Check with: sudo launchctl print system/dev.tailwatch.agent',
        },
      ],
    },
    {
      id: 'windows',
      label: 'Windows',
      intro: 'Runs as a console program or as a service. WhoIs works with the standard Tailscale client.',
      steps: [
        {
          title: 'Download and try it',
          code: [
            `Invoke-WebRequest -Uri "${RELEASES}/tailwatch-agent_windows_amd64.exe" -OutFile "$env:ProgramFiles\\tailwatch-agent.exe"`,
            ...(o.tokenConfigured ? ['$env:TAILWATCH_AGENT_TOKEN = (Get-Content C:\\tailwatch\\token.txt -Raw).Trim()'] : []),
            `& "$env:ProgramFiles\\tailwatch-agent.exe"${flagStr}`,
          ].join('\n'),
          note: 'PowerShell as Administrator.',
        },
        {
          title: 'Install as a service',
          code: [
            `sc.exe create tailwatch-agent binPath= "\\"$env:ProgramFiles\\tailwatch-agent.exe\\"${flagStr}" start= auto`,
            ...(o.tokenConfigured
              ? ['[Environment]::SetEnvironmentVariable("TAILWATCH_AGENT_TOKEN", "<same token as the hub>", "Machine")']
              : []),
            'sc.exe start tailwatch-agent',
          ].join('\n'),
          note: 'NSSM works too and gives you stdout logging.',
        },
      ],
    },
    {
      id: 'docker',
      label: 'Docker',
      intro: 'The hub image ships the agent binary, but the agent needs the host\'s /proc, /sys, mounts and tailscaled — containerising it hides most of what it measures. Prefer a host install; use this only for a hub sidecar.',
      steps: [
        {
          title: 'Run alongside the hub (deploy/docker/docker-compose.yml)',
          code: [
            'services:',
            '  tailwatch-agent:',
            '    image: ghcr.io/evilgenius79/tailwatch:latest',
            '    entrypoint: ["tailwatch-agent"]',
            ...(flags.length ? [`    command: [${flags.flatMap((f) => f.split(' ')).map((p) => `"${p}"`).join(', ')}]`] : []),
            '    network_mode: host',
            '    pid: host',
            '    read_only: true',
            '    cap_drop: [ALL]',
            '    security_opt: ["no-new-privileges:true"]',
            ...(o.tokenConfigured ? ['    env_file: [agent.env]   # TAILWATCH_AGENT_TOKEN=…'] : []),
            '    volumes:',
            '      - /var/run/tailscale/tailscaled.sock:/var/run/tailscale/tailscaled.sock',
            '      - /proc:/host/proc:ro',
            '      - /sys:/host/sys:ro',
            '    environment:',
            '      HOST_PROC: /host/proc',
            '      HOST_SYS: /host/sys',
          ].join('\n'),
          note: 'network_mode: host is required so the agent binds the host\'s Tailscale IP and WhoIs sees the real peer address.',
        },
      ],
    },
  ]
}

/** Which tag on the hub node should drive `--allow-tag` (prefers tag:tailwatch-hub, then the first tag). */
export function hubTagFor(tags: ReadonlyArray<string> | undefined): string | undefined {
  const list = (tags ?? []).filter((t) => t.startsWith('tag:'))
  if (!list.length) return undefined
  return list.find((t) => t === 'tag:tailwatch-hub') ?? list[0]
}

/** Section ids for the in-page navigation, in display order. */
export const SETTINGS_SECTIONS: readonly { id: string; label: string; adminOnly?: boolean }[] = [
  { id: 'hub', label: 'Hub' },
  { id: 'auth', label: 'Authentication' },
  { id: 'sources', label: 'Data sources' },
  { id: 'storage', label: 'Storage' },
  { id: 'notifications', label: 'Notifications' },
  { id: 'agent', label: 'Agent install' },
  { id: 'audit', label: 'Audit log', adminOnly: true },
  { id: 'about', label: 'About' },
]

export function visibleSections(isAdmin: boolean): typeof SETTINGS_SECTIONS {
  return SETTINGS_SECTIONS.filter((s) => !s.adminOnly || isAdmin)
}

/** Settings fields that only matter to the hub operator, never rendered when absent. */
export function settingsSummary(s: Settings | undefined): string {
  if (!s) return ''
  const parts = [s.demoMode ? 'demo' : authModeLabel(s.authMode), `${s.notifiers.length} notifier${s.notifiers.length === 1 ? '' : 's'}`]
  return parts.join(' · ')
}
