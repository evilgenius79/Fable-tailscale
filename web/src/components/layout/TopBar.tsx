import { Menu, RefreshCw, Search } from 'lucide-react'
import { useIsAdmin, useRefresh } from '../../api/hooks'
import { useUIStore } from '../../store'
import { IconButton } from '../ui/Button'
import { Shortcut } from '../ui/Kbd'
import { toast } from '../ui/Toast'
import { IdentityMenu } from './IdentityMenu'
import { LiveStatusIndicator } from './LiveStatus'
import { LogoMark } from './Logo'
import { ThemeToggle } from './ThemeToggle'

/** Sticky top bar: mobile menu, global search trigger, live status, refresh (admin), theme, identity. */
export function TopBar() {
  const setMobileNavOpen = useUIStore((s) => s.setMobileNavOpen)
  const setPaletteOpen = useUIStore((s) => s.setPaletteOpen)
  const isAdmin = useIsAdmin()
  const refresh = useRefresh()
  return (
    <header className="sticky top-0 z-20 flex h-[var(--topbar-h)] items-center gap-2 border-b border-border bg-bg/85 px-4 backdrop-blur sm:gap-3 sm:px-6 lg:px-8">
      <IconButton icon={Menu} label="Open navigation" size="sm" className="lg:hidden" tooltip={false} onClick={() => setMobileNavOpen(true)} />
      <a href="/" className="flex items-center rounded-md focus-ring lg:hidden" aria-label="Tailwatch home">
        <LogoMark size={22} />
      </a>
      <button
        type="button"
        onClick={() => setPaletteOpen(true)}
        className="hidden h-8 w-64 items-center gap-2 rounded-md border border-border bg-surface px-2.5 text-[13px] text-fg-muted shadow-xs transition-colors hover:border-border-strong hover:text-fg focus-ring md:flex lg:w-72"
        aria-label="Search devices (Command K)"
      >
        <Search className="size-3.5" aria-hidden="true" />
        <span className="flex-1 text-left">Search devices…</span>
        <Shortcut keys={['Mod', 'K']} />
      </button>
      <IconButton icon={Search} label="Search devices" size="sm" className="md:hidden" onClick={() => setPaletteOpen(true)} />
      <div className="ml-auto flex items-center gap-1 sm:gap-2">
        <LiveStatusIndicator />
        {isAdmin ? (
          <IconButton
            icon={RefreshCw}
            label="Poll now"
            size="sm"
            loading={refresh.isPending}
            onClick={() =>
              refresh.mutate(undefined, {
                onSuccess: () => toast.info('Refresh requested', 'The hub is polling the tailnet now.', { duration: 2500 }),
              })
            }
          />
        ) : null}
        <ThemeToggle />
        <div className="mx-0.5 hidden h-5 w-px bg-border sm:block" aria-hidden="true" />
        <IdentityMenu />
      </div>
    </header>
  )
}
