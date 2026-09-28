import { lazy } from 'react'
import { createBrowserRouter, isRouteErrorResponse, useRouteError, type RouteObject } from 'react-router-dom'
import { AppShell, type RouteHandle } from './components/layout/AppShell'
import { ErrorState } from './components/ui/ErrorState'

const OverviewPage = lazy(() => import('./pages/OverviewPage'))
const DevicesPage = lazy(() => import('./pages/DevicesPage'))
const DeviceDetailPage = lazy(() => import('./pages/DeviceDetailPage'))
const NetworkPage = lazy(() => import('./pages/NetworkPage'))
const AlertsPage = lazy(() => import('./pages/AlertsPage'))
const EventsPage = lazy(() => import('./pages/EventsPage'))
const SettingsPage = lazy(() => import('./pages/SettingsPage'))
const NotFoundPage = lazy(() => import('./pages/NotFoundPage'))

/** Rendered when a route (or lazy chunk) throws. */
export function RouteError() {
  const err = useRouteError()
  const description = isRouteErrorResponse(err) ? `${err.status} ${err.statusText}` : err instanceof Error ? err.message : 'Unexpected error'
  return (
    <div className="mx-auto max-w-lg py-16">
      <ErrorState title="Something went wrong" description={description} onRetry={() => window.location.reload()} />
    </div>
  )
}

const handle = (title: string): RouteHandle => ({ title })

export const routes: RouteObject[] = [
  {
    path: '/',
    element: <AppShell />,
    errorElement: <RouteError />,
    children: [
      { index: true, element: <OverviewPage />, handle: handle('Overview') },
      { path: 'devices', element: <DevicesPage />, handle: handle('Devices') },
      { path: 'devices/:id', element: <DeviceDetailPage />, handle: handle('Device') },
      { path: 'network', element: <NetworkPage />, handle: handle('Network') },
      { path: 'alerts', element: <AlertsPage />, handle: handle('Alerts') },
      { path: 'events', element: <EventsPage />, handle: handle('Events') },
      { path: 'settings', element: <SettingsPage />, handle: handle('Settings') },
      { path: '*', element: <NotFoundPage />, handle: handle('Not found') },
    ],
  },
]

export const router = createBrowserRouter(routes)
