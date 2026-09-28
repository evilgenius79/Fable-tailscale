import { BookOpen, ExternalLink, Info, ShieldCheck, type LucideIcon } from 'lucide-react'
import type { HubInfo } from '../../api/types'
import { SettingsSection } from './SettingsSection'

const REPO = 'https://github.com/evilgenius79/fable-tailscale'

const DOCS: { label: string; path: string; description: string }[] = [
  { label: 'README', path: '', description: 'What Tailwatch is and how to run it.' },
  { label: 'Deploying the hub', path: 'blob/main/docs/DEPLOY.md', description: 'systemd, Docker, ACL policy, control API and TLS.' },
  { label: 'Agent', path: 'blob/main/docs/AGENT.md', description: 'tailwatch-agent flags, authentication and install.' },
  { label: 'HTTP API', path: 'blob/main/docs/API.md', description: 'Every endpoint, the SSE stream and rule parameters.' },
  { label: 'Security', path: 'blob/main/docs/SECURITY.md', description: 'Threat model, hardening and how to report issues.' },
  { label: 'Architecture', path: 'blob/main/docs/ARCHITECTURE.md', description: 'Packages and contracts for contributors.' },
]

const SECURITY_NOTES: { icon: LucideIcon; text: string }[] = [
  { icon: ShieldCheck, text: 'No passwords, sessions or cookies: identity comes from tailscaled WhoIs on the source IP, and the hub listens on its Tailscale IP only.' },
  { icon: ShieldCheck, text: 'Cross-site requests are refused (Sec-Fetch-Site, Origin checks); every non-GET call must carry X-Requested-With: tailwatch and no CORS headers are ever sent.' },
  { icon: ShieldCheck, text: 'Strict CSP (same-origin scripts, styles and fonts; no inline scripts), nosniff, frame-ancestors none, no-referrer, and Cache-Control: no-store on the API.' },
  { icon: ShieldCheck, text: 'Per-identity rate limiting (20 req/s, burst 60), 64 KiB request bodies and short server timeouts.' },
  { icon: ShieldCheck, text: 'Secrets (API keys, tokens, webhook URLs) are never returned by the API, never logged and never stored in the database.' },
  { icon: ShieldCheck, text: 'Every admin action is recorded in the audit log and emitted as an admin.action event, whether it succeeded or not.' },
]

export function AboutSection({ hub }: { hub: HubInfo | null | undefined }) {
  return (
    <SettingsSection id="about" icon={Info} title="About" description={hub ? `Tailwatch ${hub.version} · a Tailscale network viewer and watchdog.` : 'Tailwatch · a Tailscale network viewer and watchdog.'}>
      <div className="grid gap-6 lg:grid-cols-2">
        <div>
          <p className="mb-2 flex items-center gap-1.5 text-[11px] font-semibold uppercase tracking-wider text-fg-muted">
            <BookOpen className="size-3.5" aria-hidden="true" />
            Documentation
          </p>
          <ul className="divide-y divide-border-subtle rounded-lg border border-border">
            {DOCS.map((d) => (
              <li key={d.label}>
                <a
                  href={d.path ? `${REPO}/${d.path}` : REPO}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="flex items-center gap-3 px-3 py-2.5 transition-colors hover:bg-surface-hover focus-ring-inset"
                >
                  <span className="min-w-0 flex-1">
                    <span className="block text-[13px] font-medium text-fg">{d.label}</span>
                    <span className="block truncate text-xs text-fg-muted">{d.description}</span>
                  </span>
                  <ExternalLink className="size-3.5 shrink-0 text-fg-faint" aria-hidden="true" />
                </a>
              </li>
            ))}
          </ul>
        </div>
        <div>
          <p className="mb-2 flex items-center gap-1.5 text-[11px] font-semibold uppercase tracking-wider text-fg-muted">
            <ShieldCheck className="size-3.5" aria-hidden="true" />
            Security notes
          </p>
          <ul className="space-y-2.5">
            {SECURITY_NOTES.map((n, i) => {
              const Icon = n.icon
              return (
                <li key={i} className="flex gap-2.5 text-[13px] leading-5 text-fg-secondary">
                  <Icon className="mt-0.5 size-4 shrink-0 text-online" aria-hidden="true" />
                  <span>{n.text}</span>
                </li>
              )
            })}
          </ul>
        </div>
      </div>
    </SettingsSection>
  )
}
