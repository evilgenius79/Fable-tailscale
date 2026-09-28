import { useRef } from 'react'
import { NavLink } from 'react-router-dom'
import { Activity, Bell, LayoutDashboard, MonitorSmartphone, PanelLeftClose, PanelLeftOpen, Settings, Waypoints, X } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { cn } from '../../lib/cn'
import { formatCompact } from '../../lib/format'
import { useOpenAlertCount, useOverview } from '../../api/hooks'
import { useFocusTrap } from '../../lib/useFocusTrap'
import { useUIStore } from '../../store'
import { Tooltip } from '../ui/Tooltip'
import { IconButton } from '../ui/Button'
import { LogoMark, Wordmark } from './Logo'

export interface NavItem {
  to: string
  label: string
  icon: LucideIcon
  end?: boolean
}

export const NAV_ITEMS: readonly NavItem[] = [
  { to: '/', label: 'Overview', icon: LayoutDashboard, end: true },
  { to: '/devices', label: 'Devices', icon: MonitorSmartphone },
  { to: '/network', label: 'Network', icon: Waypoints },
  { to: '/alerts', label: 'Alerts', icon: Bell },
  { to: '/events', label: 'Events', icon: Activity },
  { to: '/settings', label: 'Settings', icon: Settings },
]

function NavRow({ item, collapsed, badge, onNavigate }: { item: NavItem; collapsed: boolean; badge?: number; onNavigate?: () => void }) {
  const Icon = item.icon
  const link = (
    <NavLink
      to={item.to}
      end={item.end}
      onClick={onNavigate}
      aria-label={collapsed ? item.label : undefined}
      className={({ isActive }) =>
        cn(
          'group relative flex h-9 items-center gap-2.5 rounded-md text-[13px] font-medium transition-colors focus-ring',
          collapsed ? 'justify-center px-0' : 'px-2.5',
          isActive ? 'bg-surface-active text-fg' : 'text-fg-secondary hover:bg-surface-hover hover:text-fg',
        )
      }
    >
      {({ isActive }) => (
        <>
          {isActive ? <span aria-hidden="true" className="absolute -left-2 top-1/2 h-4 w-0.5 -translate-y-1/2 rounded-full bg-accent" /> : null}
          <span className="relative">
            <Icon className={cn('size-4', isActive ? 'text-fg' : 'text-fg-muted group-hover:text-fg')} aria-hidden="true" />
            {collapsed && badge ? <span aria-hidden="true" className="absolute -right-1 -top-1 size-2 rounded-full bg-critical-fill ring-2 ring-surface" /> : null}
          </span>
          {!collapsed ? <span className="truncate">{item.label}</span> : null}
          {!collapsed && badge ? (
            <span className="ml-auto rounded-md bg-critical-soft px-1.5 py-0.5 text-[11px] font-semibold leading-none text-critical num" aria-label={`${badge} open`}>
              {formatCompact(badge)}
            </span>
          ) : null}
          {collapsed && badge ? <span className="sr-only">{badge} open alerts</span> : null}
        </>
      )}
    </NavLink>
  )
  return collapsed ? (
    <Tooltip content={badge ? `${item.label} · ${badge} open` : item.label} side="right" delay={0}>
      {link}
    </Tooltip>
  ) : (
    link
  )
}

export function SidebarNav({ collapsed, onNavigate }: { collapsed: boolean; onNavigate?: () => void }) {
  const alerts = useOpenAlertCount()
  return (
    <nav aria-label="Primary" className={cn('flex flex-col gap-0.5', collapsed ? 'px-3' : 'px-3')}>
      {NAV_ITEMS.map((item) => (
        <NavRow key={item.to} item={item} collapsed={collapsed} badge={item.to === '/alerts' ? alerts : undefined} onNavigate={onNavigate} />
      ))}
    </nav>
  )
}

function HubFooter({ collapsed }: { collapsed: boolean }) {
  const streamHub = useUIStore((s) => s.hub)
  // The store is only fed by the SSE hello/tick; when the stream is blocked or
  // slow, the overview (already fetched by every page) carries the same HubInfo.
  const overview = useOverview()
  const hub = streamHub ?? overview.data?.hub ?? null
  // Nothing (rather than '—' / 'v—') until either source has delivered it.
  if (collapsed || !hub) return null
  return (
    <div className="px-4 pb-3 text-[11px] leading-4 text-fg-muted">
      <p className="truncate font-medium text-fg-secondary">{hub.tailnet}</p>
      <p className="truncate">
        v{hub.version}
        {hub.demoMode ? ' · demo' : ''}
      </p>
    </div>
  )
}

/** Desktop sidebar (fixed, collapsible to an icon rail). Hidden below lg. */
export function Sidebar() {
  const collapsed = useUIStore((s) => s.sidebarCollapsed)
  const toggle = useUIStore((s) => s.toggleSidebar)
  return (
    <aside
      className={cn(
        'fixed inset-y-0 left-0 z-30 hidden flex-col border-r border-border bg-surface transition-[width] duration-150 ease-out lg:flex',
        collapsed ? 'w-[var(--sidebar-rail-w)]' : 'w-[var(--sidebar-w)]',
      )}
      aria-label="Sidebar"
    >
      <div className={cn('flex h-[var(--topbar-h)] shrink-0 items-center border-b border-border', collapsed ? 'justify-center px-0' : 'gap-2.5 px-4')}>
        <LogoMark />
        {!collapsed ? <Wordmark /> : null}
      </div>
      <div className="flex-1 overflow-y-auto py-3 scrollbar-none">
        <SidebarNav collapsed={collapsed} />
      </div>
      <div className={cn('border-t border-border pt-3', collapsed ? 'flex flex-col items-center gap-2 pb-3' : '')}>
        <HubFooter collapsed={collapsed} />
        <div className={cn(collapsed ? '' : 'px-3 pb-3')}>
          <IconButton
            icon={collapsed ? PanelLeftOpen : PanelLeftClose}
            label={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
            tooltipSide="right"
            size="sm"
            onClick={toggle}
            className={cn(!collapsed && 'w-full justify-start px-2.5 gap-2')}
          >
            {!collapsed ? <span className="text-[13px] font-medium text-fg-secondary">Collapse</span> : null}
          </IconButton>
        </div>
      </div>
    </aside>
  )
}

/** Mobile drawer (below lg). */
export function MobileDrawer() {
  const open = useUIStore((s) => s.mobileNavOpen)
  const setOpen = useUIStore((s) => s.setMobileNavOpen)
  const panelRef = useRef<HTMLDivElement>(null)
  useFocusTrap(panelRef, open)
  if (!open) return null
  return (
    <div className="fixed inset-0 z-40 lg:hidden" role="dialog" aria-modal="true" aria-label="Navigation">
      <div className="absolute inset-0 bg-overlay animate-fade-in" onClick={() => setOpen(false)} aria-hidden="true" />
      <div ref={panelRef} className="absolute inset-y-0 left-0 flex w-[280px] max-w-[85vw] flex-col border-r border-border bg-surface shadow-overlay animate-slide-in-left" onKeyDown={(e) => e.key === 'Escape' && setOpen(false)}>
        <div className="flex h-[var(--topbar-h)] shrink-0 items-center justify-between border-b border-border pl-4 pr-2">
          <div className="flex items-center gap-2.5">
            <LogoMark />
            <Wordmark />
          </div>
          <IconButton icon={X} label="Close navigation" size="sm" tooltip={false} onClick={() => setOpen(false)} autoFocus />
        </div>
        <div className="flex-1 overflow-y-auto py-3">
          <SidebarNav collapsed={false} onNavigate={() => setOpen(false)} />
        </div>
        <div className="border-t border-border pt-3">
          <HubFooter collapsed={false} />
        </div>
      </div>
    </div>
  )
}
