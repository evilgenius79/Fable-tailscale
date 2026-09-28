import { ChevronDown, Command, Settings, ShieldCheck, User } from 'lucide-react'
import { useMe } from '../../api/hooks'
import { cn } from '../../lib/cn'
import { useUIStore } from '../../store'
import { Badge } from '../ui/Badge'
import { DropdownMenu } from '../ui/Menu'
import { Skeleton } from '../ui/Skeleton'
import { isMacPlatform } from '../ui/Kbd'

function initials(name: string): string {
  const parts = name.replace(/@.*$/, '').split(/[\s._-]+/).filter(Boolean)
  const a = parts[0]?.[0] ?? '?'
  const b = parts.length > 1 ? parts[parts.length - 1]?.[0] ?? '' : ''
  return (a + b).toUpperCase()
}

/** Avatar + name trigger showing login / role / node from GET /api/v1/me. */
export function IdentityMenu() {
  const { data: me, isPending, isError } = useMe()
  const setPaletteOpen = useUIStore((s) => s.setPaletteOpen)
  const name = me?.displayName || me?.login || (isError ? 'Unknown' : '')
  const trigger = (
    <button
      type="button"
      className="flex h-8 items-center gap-2 rounded-md pl-1 pr-1.5 text-[13px] font-medium text-fg hover:bg-surface-hover focus-ring"
      aria-label={me ? `Account: ${name} (${me.role})` : 'Account'}
    >
      <span
        className={cn(
          'flex size-6 items-center justify-center rounded-full text-[10px] font-semibold',
          me ? 'bg-accent-soft text-accent-text' : 'bg-surface-inset text-fg-muted',
        )}
        aria-hidden="true"
      >
        {isPending ? <Skeleton width={12} height={8} rounded="full" /> : me ? initials(name) : <User className="size-3.5" />}
      </span>
      <span className="hidden max-w-[140px] truncate md:inline">{isPending ? <Skeleton width={72} height={10} /> : name}</span>
      <ChevronDown className="hidden size-3.5 text-fg-muted md:inline" aria-hidden="true" />
    </button>
  )
  const header = me ? (
    <div className="min-w-0">
      <p className="truncate text-[13px] font-semibold text-fg">{me.displayName || me.login}</p>
      <p className="truncate text-xs text-fg-muted">{me.login}</p>
      <div className="mt-1.5 flex flex-wrap items-center gap-1.5">
        <Badge tone={me.role === 'admin' ? 'accent' : 'neutral'} size="sm" icon={me.role === 'admin' ? ShieldCheck : undefined}>
          {me.role}
        </Badge>
        {me.authMode === 'none' ? (
          <Badge tone="warning" size="sm">
            no auth
          </Badge>
        ) : null}
      </div>
      <p className="mt-1.5 truncate font-mono text-[11px] text-fg-muted">
        {me.nodeName} · {me.nodeIp}
      </p>
    </div>
  ) : (
    <p className="text-xs text-fg-muted">{isError ? 'Identity unavailable' : 'Loading identity…'}</p>
  )
  return (
    <DropdownMenu
      label="Account"
      align="end"
      width={240}
      header={header}
      items={[
        { id: 'palette', label: 'Search devices', icon: Command, hint: isMacPlatform() ? '⌘K' : 'Ctrl K', onSelect: () => setPaletteOpen(true) },
        { id: 'settings', label: 'Settings', icon: Settings, to: '/settings' },
      ]}
      trigger={trigger}
    />
  )
}
