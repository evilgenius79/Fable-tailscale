import { Link } from 'react-router-dom'
import { Clock, Globe, MonitorSmartphone, Server } from 'lucide-react'
import { useDevices, useOverview } from '../api/hooks'
import { FleetBreakdownCard } from '../components/overview/FleetBreakdownCard'
import { HubBanners } from '../components/overview/HubBanners'
import { KpiRow } from '../components/overview/KpiRow'
import { OpenAlertsCard } from '../components/overview/OpenAlertsCard'
import { RecentEventsCard } from '../components/overview/RecentEventsCard'
import { ThroughputChart } from '../components/overview/ThroughputChart'
import { TopDevicesCard } from '../components/overview/TopDevicesCard'
import { Badge } from '../components/ui/Badge'
import { buttonClass } from '../components/ui/Button'
import { ErrorState } from '../components/ui/ErrorState'
import { PageHeader } from '../components/ui/PageHeader'
import { formatDateTime, formatRelative } from '../lib/format'
import { useUIStore } from '../store'

export default function OverviewPage() {
  const ov = useOverview()
  const devices = useDevices()
  const hubFromStream = useUIStore((s) => s.hub)
  const hub = ov.data?.hub ?? hubFromStream
  const loading = ov.isPending
  const fetching = ov.isFetching && !ov.isPending

  return (
    <>
      <PageHeader
        title="Overview"
        description="Fleet health, connectivity and traffic across your tailnet at a glance."
        meta={
          hub ? (
            <>
              <span className="inline-flex items-center gap-1.5">
                <Globe className="size-3.5 text-fg-muted" aria-hidden="true" />
                <span className="sr-only">Tailnet </span>
                <span className="font-medium text-fg">{hub.tailnet}</span>
              </span>
              <span className="inline-flex items-center gap-1.5">
                <Server className="size-3.5 text-fg-muted" aria-hidden="true" />
                <span className="sr-only">Hub node </span>
                <Link to={`/devices/${encodeURIComponent(hub.selfId)}`} className="rounded font-medium text-fg hover:text-accent-text focus-ring">
                  {hub.selfName}
                </Link>
              </span>
              <span className="inline-flex items-center gap-1.5" title={formatDateTime(hub.lastPoll)}>
                <Clock className="size-3.5 text-fg-muted" aria-hidden="true" />
                Last poll <span className="num text-fg">{formatRelative(hub.lastPoll)}</span>
              </span>
              {hub.demoMode ? (
                <Badge size="sm" tone="info">
                  Demo data
                </Badge>
              ) : null}
            </>
          ) : null
        }
        actions={
          <Link to="/devices" className={buttonClass({ size: 'sm' })}>
            <MonitorSmartphone aria-hidden="true" />
            All devices
          </Link>
        }
      />

      <HubBanners hub={hub} />

      {ov.error && !ov.data ? (
        <ErrorState error={ov.error} onRetry={() => void ov.refetch()} retrying={ov.isFetching} className="mb-6" />
      ) : (
        <KpiRow overview={ov.data} loading={loading} />
      )}

      <div className="mt-4 grid gap-4 xl:mt-6 xl:grid-cols-3 xl:gap-6">
        <div className="min-w-0 space-y-4 xl:col-span-2 xl:space-y-6">
          <ThroughputChart overview={ov.data} loading={loading} fetching={fetching} error={ov.data ? undefined : ov.error} onRetry={() => void ov.refetch()} />
          <TopDevicesCard devices={devices.data} loading={devices.isPending} error={devices.error} onRetry={() => void devices.refetch()} />
          <FleetBreakdownCard overview={ov.data} loading={loading} />
        </div>
        <div className="min-w-0 space-y-4 xl:space-y-6">
          <OpenAlertsCard limit={5} />
          <RecentEventsCard limit={8} />
        </div>
      </div>
    </>
  )
}
