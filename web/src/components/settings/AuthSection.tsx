import type { ReactNode } from 'react'
import { ShieldCheck, User, Users } from 'lucide-react'
import type { Identity, Settings } from '../../api/types'
import { cn } from '../../lib/cn'
import { Badge } from '../ui/Badge'
import { KeyValueList } from '../ui/KeyValue'
import { SkeletonText } from '../ui/Skeleton'
import { Tag } from '../ui/Tag'
import { SettingsSection } from './SettingsSection'
import { authModeLabel, principalsLabel } from './settings'

function initials(name: string): string {
  const parts = name.replace(/@.*$/, '').split(/[\s._-]+/).filter(Boolean)
  const a = parts[0]?.[0] ?? '?'
  const b = parts.length > 1 ? parts[parts.length - 1]?.[0] ?? '' : ''
  return (a + b).toUpperCase()
}

function Principals({ title, list, kind, hint }: { title: string; list: ReadonlyArray<string> | undefined; kind: 'users' | 'tags'; hint: string }) {
  const p = principalsLabel(list)
  let body: ReactNode
  if (p.kind === 'everyone') body = <span className="text-[13px] text-fg">Everyone on the tailnet</span>
  else if (p.kind === 'nobody') body = <span className="text-[13px] text-fg-faint">Nobody</span>
  else
    body = (
      <ul className="flex flex-wrap gap-1">
        {p.items.map((it) =>
          kind === 'tags' ? (
            <li key={it}>
              <Tag tag={it} />
            </li>
          ) : (
            <li key={it} className="inline-flex h-6 max-w-full items-center rounded-md border border-border bg-surface-inset px-2 text-xs text-fg">
              <span className="truncate">{it}</span>
            </li>
          ),
        )}
      </ul>
    )
  return (
    <div className="min-w-0 rounded-lg border border-border p-3">
      <p className="text-[11px] font-semibold uppercase tracking-wider text-fg-muted">{title}</p>
      <div className="mt-1.5">{body}</div>
      <p className="mt-1.5 text-[11px] text-fg-faint">{hint}</p>
    </div>
  )
}

export interface AuthSectionProps {
  settings: Settings | undefined
  me: Identity | undefined
  loading: boolean
}

export function AuthSection({ settings, me, loading }: AuthSectionProps) {
  return (
    <SettingsSection id="auth" icon={ShieldCheck} title="Authentication" description="No passwords or sessions: every request is attributed to a tailnet identity by asking tailscaled who is behind the source IP.">
      {loading && !settings ? (
        <SkeletonText lines={5} />
      ) : (
        <div className="space-y-5">
          <KeyValueList
            columns={2}
            items={[
              {
                key: 'Mode',
                value: (
                  <span className="inline-flex items-center gap-1.5">
                    {authModeLabel(settings?.authMode)}
                    {settings?.authMode === 'none' ? (
                      <Badge size="sm" tone="warning">
                        open
                      </Badge>
                    ) : null}
                  </span>
                ),
              },
              {
                key: 'Admin actions',
                value: (
                  <Badge size="sm" tone={settings?.adminActionsEnabled ? 'online' : 'neutral'} dot>
                    {settings?.adminActionsEnabled ? 'Enabled' : 'Disabled'}
                  </Badge>
                ),
                hint: 'Requires --enable-admin-actions and a control API credential',
              },
            ]}
          />
          <div className="grid gap-3 sm:grid-cols-2">
            <Principals title="Admins" list={settings?.adminUsers} kind="users" hint="--admins · login names granted the admin role" />
            <Principals title="Admin tags" list={settings?.adminTags} kind="tags" hint="--admin-tags · tagged nodes granted the admin role" />
            <Principals title="Viewers" list={settings?.viewerUsers} kind="users" hint="--viewers · default * lets any tailnet identity view" />
            <Principals title="Viewer tags" list={settings?.viewerTags} kind="tags" hint="--viewer-tags · tagged nodes granted the viewer role" />
          </div>
          <div className="flex items-start gap-3 rounded-lg border border-border bg-surface-inset p-3 sm:p-4">
            <span className={cn('flex size-10 shrink-0 items-center justify-center rounded-full text-sm font-semibold', me ? 'bg-accent-soft text-accent-text' : 'bg-surface-hover text-fg-muted')} aria-hidden="true">
              {me ? initials(me.displayName || me.login) : <User className="size-4" />}
            </span>
            <div className="min-w-0 flex-1">
              <p className="flex flex-wrap items-center gap-2 text-[13px] font-semibold text-fg">
                <span className="truncate">{me ? me.displayName || me.login : 'Identity unavailable'}</span>
                {me ? (
                  <Badge size="sm" tone={me.role === 'admin' ? 'accent' : 'neutral'} icon={me.role === 'admin' ? ShieldCheck : Users}>
                    {me.role}
                  </Badge>
                ) : null}
                <span className="text-xs font-normal text-fg-muted">(you)</span>
              </p>
              {me ? (
                <>
                  <p className="truncate text-xs text-fg-secondary">{me.login}</p>
                  <p className="mt-1 truncate font-mono text-[11px] text-fg-muted">
                    {me.nodeName} · {me.nodeIp}
                  </p>
                  {me.tags?.length ? (
                    <ul className="mt-1.5 flex flex-wrap gap-1">
                      {me.tags.map((t) => (
                        <li key={t}>
                          <Tag tag={t} size="sm" />
                        </li>
                      ))}
                    </ul>
                  ) : null}
                </>
              ) : (
                <p className="text-xs text-fg-muted">The hub could not attribute this session.</p>
              )}
            </div>
          </div>
        </div>
      )}
    </SettingsSection>
  )
}
