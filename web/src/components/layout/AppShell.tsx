import { Suspense, useEffect } from 'react'
import { Outlet, useLocation, useMatches } from 'react-router-dom'
import { cn } from '../../lib/cn'
import { useDocumentTitle } from '../../lib/useDocumentTitle'
import { useUIStore } from '../../store'
import { PageSkeleton } from '../ui/Skeleton'
import { CommandPalette } from './CommandPalette'
import { MobileDrawer, Sidebar } from './Sidebar'
import { TopBar } from './TopBar'

export interface RouteHandle {
  title?: string
}

function useRouteTitle(): string | undefined {
  const matches = useMatches()
  for (let i = matches.length - 1; i >= 0; i--) {
    const h = matches[i]?.handle as RouteHandle | undefined
    if (h?.title) return h.title
  }
  return undefined
}

/**
 * Layout route: fixed sidebar (collapsible rail), mobile drawer, sticky top
 * bar, command palette and the page outlet. Pages render inside a
 * max-width container with responsive gutters.
 */
export function AppShell() {
  const collapsed = useUIStore((s) => s.sidebarCollapsed)
  const setMobileNavOpen = useUIStore((s) => s.setMobileNavOpen)
  const location = useLocation()
  const title = useRouteTitle()
  useDocumentTitle(title)

  // Close the drawer on navigation.
  useEffect(() => setMobileNavOpen(false), [location.pathname, setMobileNavOpen])

  return (
    <div className="min-h-dvh bg-bg text-fg">
      <a
        href="#main"
        className="sr-only focus:not-sr-only focus:fixed focus:left-4 focus:top-4 focus:z-[120] focus:rounded-md focus:bg-accent focus:px-3 focus:py-2 focus:text-sm focus:font-medium focus:text-accent-fg"
      >
        Skip to content
      </a>
      <Sidebar />
      <MobileDrawer />
      <div className={cn('flex min-h-dvh min-w-0 flex-col transition-[padding] duration-150 ease-out', collapsed ? 'lg:pl-[var(--sidebar-rail-w)]' : 'lg:pl-[var(--sidebar-w)]')}>
        <TopBar />
        <main id="main" className="flex-1 px-4 py-5 sm:px-6 sm:py-6 lg:px-8" tabIndex={-1}>
          <div className="mx-auto w-full max-w-[1600px] min-w-0">
            <Suspense fallback={<PageSkeleton />}>
              <Outlet />
            </Suspense>
          </div>
        </main>
      </div>
      <CommandPalette />
    </div>
  )
}
