import { useCallback, useMemo, useState } from 'react'
import { Link, useParams, useSearchParams } from 'react-router-dom'
import { ArrowLeft, Bell, Compass, History, LayoutDashboard, Server, Waypoints } from 'lucide-react'
import { isApiError } from '../api/client'
import { useDevice, useIsAdmin, useSeries, useSettings, useUptime } from '../api/hooks'
import { DeviceActions } from '../components/device/DeviceActions'
import { DeviceMeta, DeviceSubtitle, DeviceTitle } from '../components/device/DeviceMeta'
import type { DeviceDialogKind } from '../components/device/DeviceDialogs'
import { ConnectivityTab } from '../components/device/ConnectivityTab'
import { DeviceAlerts, DeviceEvents } from '../components/device/ActivityTabs'
import { OverviewTab } from '../components/device/OverviewTab'
import { SystemTab } from '../components/device/SystemTab'
import { buttonClass } from '../components/ui/Button'
import { EmptyState } from '../components/ui/EmptyState'
import { ErrorState } from '../components/ui/ErrorState'
import { PageHeader } from '../components/ui/PageHeader'
import { Skeleton, SkeletonCard } from '../components/ui/Skeleton'
import { TabPanel, Tabs } from '../components/ui/Tabs'
import { TimeRangePicker } from '../components/ui/TimeRangePicker'
import { deviceIcon } from '../lib/os'
import type { RangeKey } from '../lib/time'
import { useDocumentTitle } from '../lib/useDocumentTitle'
import { useUIStore } from '../store'

type TabId = 'overview' | 'connectivity' | 'system' | 'events' | 'alerts'
const TAB_IDS: TabId[] = ['overview', 'connectivity', 'system', 'events', 'alerts']
const RANGE_OPTIONS: RangeKey[] = ['15m', '1h', '6h', '24h', '7d', '30d']

const backCrumb = {
  label: (
    <span className="inline-flex items-center gap-1">
      <ArrowLeft className="size-3" aria-hidden="true" />
      Devices
    </span>
  ),
  to: '/devices',
}

function DetailSkeleton() {
  return (
    <div className="animate-fade-in" aria-busy="true" aria-label="Loading device">
      <div className="mb-6 space-y-3">
        <Skeleton height={12} width={120} />
        <Skeleton height={28} width={260} />
        <Skeleton height={12} width={320} />
        <div className="flex gap-2">
          <Skeleton height={22} width={80} rounded="md" />
          <Skeleton height={22} width={120} rounded="md" />
          <Skeleton height={22} width={100} rounded="md" />
        </div>
      </div>
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 xl:grid-cols-4 2xl:grid-cols-6">
        {Array.from({ length: 8 }, (_, i) => (
          <SkeletonCard key={i} lines={1} />
        ))}
      </div>
      <div className="mt-4 grid gap-4 lg:grid-cols-2">
        <SkeletonCard lines={6} />
        <SkeletonCard lines={6} />
      </div>
    </div>
  )
}

export default function DeviceDetailPage() {
  const { id = '' } = useParams<{ id: string }>()
  const [params, setParams] = useSearchParams()
  const tabParam = params.get('tab')
  const tab: TabId = TAB_IDS.includes(tabParam as TabId) ? (tabParam as TabId) : 'overview'
  const setTab = useCallback(
    (next: TabId) => {
      const sp = new URLSearchParams(params)
      if (next === 'overview') sp.delete('tab')
      else sp.set('tab', next)
      setParams(sp, { replace: true })
    },
    [params, setParams],
  )

  const range = useUIStore((s) => s.range)
  const setRange = useUIStore((s) => s.setRange)
  const q = useDevice(id)
  const settings = useSettings()
  const isAdmin = useIsAdmin()
  const series = useSeries(id, range, { enabled: tab === 'overview' })
  const uptime = useUptime(id, range, { enabled: tab === 'overview' })
  const [requestedDialog, setRequestedDialog] = useState<DeviceDialogKind | null>(null)

  const detail = q.data
  const device = detail?.device
  useDocumentTitle(device?.name ?? 'Device')

  const canManage = isAdmin && !!settings.data?.adminActionsEnabled
  const agentPort = settings.data?.agentPort ?? 41820
  const agentEnabled = settings.data?.agentEnabled ?? true
  const openAlerts = useMemo(() => (detail?.openAlerts ?? []).filter((a) => a.state === 'open'), [detail?.openAlerts])

  if (q.error && !device) {
    const notFound = isApiError(q.error) && q.error.isNotFound
    return (
      <>
        <PageHeader breadcrumbs={[backCrumb, { label: id || 'Device' }]} title={notFound ? 'Device not found' : 'Device'} compact />
        {notFound ? (
          <EmptyState
            icon={Compass}
            size="lg"
            title="Device not found"
            description={
              <>
                No device with the id or name <span className="font-mono text-fg">{id}</span> exists on this tailnet. It may have been removed.
              </>
            }
            action={
              <Link to="/devices" className={buttonClass({ variant: 'primary' })}>
                Back to devices
              </Link>
            }
          />
        ) : (
          <ErrorState error={q.error} onRetry={() => void q.refetch()} retrying={q.isFetching} />
        )}
      </>
    )
  }

  if (!device) return <DetailSkeleton />

  const Icon = deviceIcon(device)
  const tabs = [
    { id: 'overview' as const, label: 'Overview', icon: LayoutDashboard },
    { id: 'connectivity' as const, label: 'Connectivity', icon: Waypoints },
    { id: 'system' as const, label: 'System', icon: Server },
    { id: 'events' as const, label: 'Events', icon: History },
    { id: 'alerts' as const, label: 'Alerts', icon: Bell, count: openAlerts.length },
  ]

  return (
    <>
      <PageHeader
        breadcrumbs={[backCrumb, { label: device.name }]}
        icon={Icon}
        title={<DeviceTitle device={device} />}
        description={<DeviceSubtitle device={device} />}
        meta={<DeviceMeta device={device} />}
        actions={<DeviceActions device={device} canManage={canManage} requestedDialog={requestedDialog} onDialogHandled={() => setRequestedDialog(null)} />}
        compact
      >
        <div className="flex flex-wrap items-end justify-between gap-x-4 gap-y-2">
          <Tabs tabs={tabs} value={tab} onValueChange={setTab} aria-label="Device sections" idPrefix="device-tab" className="min-w-0 flex-1" />
          {tab === 'overview' ? (
            <div className="pb-1.5">
              <TimeRangePicker value={range} onValueChange={setRange} options={RANGE_OPTIONS} aria-label="Chart time range" />
            </div>
          ) : null}
        </div>
      </PageHeader>

      {q.error && device ? <ErrorState compact error={q.error} title="Live data may be stale" onRetry={() => void q.refetch()} retrying={q.isFetching} className="mb-4" /> : null}

      <TabPanel id="overview" active={tab === 'overview'} idPrefix="device-tab">
        <OverviewTab device={device} range={range} series={series} uptime={uptime} agentPort={agentPort} agentEnabled={agentEnabled} />
      </TabPanel>
      <TabPanel id="connectivity" active={tab === 'connectivity'} idPrefix="device-tab">
        <ConnectivityTab device={device} canManage={canManage} onApproveRoutes={() => setRequestedDialog('routes')} />
      </TabPanel>
      <TabPanel id="system" active={tab === 'system'} idPrefix="device-tab">
        <SystemTab device={device} agentPort={agentPort} agentEnabled={agentEnabled} />
      </TabPanel>
      <TabPanel id="events" active={tab === 'events'} idPrefix="device-tab">
        <DeviceEvents id={device.id} deviceName={device.name} />
      </TabPanel>
      <TabPanel id="alerts" active={tab === 'alerts'} idPrefix="device-tab">
        <DeviceAlerts alerts={openAlerts} isAdmin={isAdmin} deviceId={device.id} deviceName={device.name} />
      </TabPanel>
    </>
  )
}
