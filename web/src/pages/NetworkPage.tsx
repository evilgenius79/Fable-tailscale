import { useMemo, useState } from 'react'
import { Waypoints } from 'lucide-react'
import { useDevices, useRules, useTopology } from '../api/hooks'
import { DerpUsageCard, LatencyDistributionCard, PathSummaryCard, SlowestLinksCard } from '../components/network/NetworkSidePanel'
import { TopologyMap } from '../components/network/TopologyMap'
import { DEFAULT_TOPOLOGY_FILTERS, filterTopology, pathSummary, type PathFilter, type TopologyFilters } from '../components/network/topology'
import { Card } from '../components/ui/Card'
import { ErrorState } from '../components/ui/ErrorState'
import { PageHeader } from '../components/ui/PageHeader'
import { SegmentedControl } from '../components/ui/SegmentedControl'
import { Toggle } from '../components/ui/Toggle'
import { formatInt, plural } from '../lib/format'

const PATH_OPTIONS: { value: PathFilter; label: string }[] = [
  { value: 'all', label: 'All paths' },
  { value: 'direct', label: 'Direct' },
  { value: 'relay', label: 'Relayed' },
]

export default function NetworkPage() {
  const topo = useTopology()
  const devices = useDevices()
  const rules = useRules()
  const [filters, setFilters] = useState<TopologyFilters>(DEFAULT_TOPOLOGY_FILTERS)

  const allNodes = useMemo(() => topo.data?.nodes ?? [], [topo.data])
  const filtered = useMemo(() => (topo.data ? filterTopology(topo.data, filters) : { nodes: [], edges: [] }), [topo.data, filters])
  const summary = useMemo(() => pathSummary(allNodes), [allNodes])
  const regions = useMemo(() => topo.data?.derpRegions ?? {}, [topo.data])
  const latencyRule = rules.data?.find((r) => r.type === 'high_latency' && r.enabled)
  const loading = topo.isPending
  const fetching = topo.isFetching && !topo.isPending

  return (
    <>
      <PageHeader
        title="Network"
        description="How every peer reaches the hub: direct paths, DERP relays and the slow spots."
        icon={Waypoints}
        meta={
          topo.data ? (
            <>
              <span className="num">{plural(summary.total, 'device')}</span>
              <span aria-hidden="true">·</span>
              <span className="inline-flex items-center gap-1.5">
                <span aria-hidden="true" className="size-1.5 rounded-full bg-direct-fill" />
                <span className="num">{formatInt(summary.direct)} direct</span>
              </span>
              <span className="inline-flex items-center gap-1.5">
                <span aria-hidden="true" className="size-1.5 rounded-full bg-relay-fill" />
                <span className="num">{formatInt(summary.relay)} relayed</span>
              </span>
              <span className="inline-flex items-center gap-1.5">
                <span aria-hidden="true" className="size-1.5 rounded-full bg-offline-fill" />
                <span className="num">{formatInt(summary.offline)} offline</span>
              </span>
            </>
          ) : null
        }
        actions={
          <>
            <SegmentedControl aria-label="Filter by path" size="sm" options={PATH_OPTIONS} value={filters.path} onValueChange={(path) => setFilters((f) => ({ ...f, path }))} />
            <Toggle size="sm" label="Online only" checked={filters.onlineOnly} onCheckedChange={(onlineOnly) => setFilters((f) => ({ ...f, onlineOnly }))} labelPosition="end" className="rounded-md border border-border bg-surface-raised px-2.5 py-1.5 shadow-xs" />
          </>
        }
      />

      {topo.error && topo.data ? <ErrorState compact error={topo.error} title="Topology may be stale" onRetry={() => void topo.refetch()} retrying={topo.isFetching} className="mb-4" /> : null}

      <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_380px] xl:gap-6 2xl:grid-cols-[minmax(0,1fr)_420px]">
        <Card padding="none" className="min-w-0 overflow-hidden xl:sticky xl:top-[calc(var(--topbar-h)+16px)] xl:self-start">
          <div className="h-[440px] sm:h-[560px] xl:h-[calc(100dvh-var(--topbar-h)-32px)] xl:min-h-[560px] xl:max-h-[900px]">
            <TopologyMap
              nodes={filtered.nodes}
              edges={filtered.edges}
              loading={loading}
              fetching={fetching}
              error={topo.data ? undefined : topo.error}
              onRetry={() => void topo.refetch()}
              unfilteredCount={allNodes.length}
              className="rounded-lg"
            />
          </div>
        </Card>

        <div className="grid min-w-0 gap-4 sm:grid-cols-2 xl:grid-cols-1 xl:gap-6">
          <PathSummaryCard nodes={allNodes} loading={loading} />
          <LatencyDistributionCard nodes={allNodes} loading={loading} />
          <div className="sm:col-span-2 xl:col-span-1">
            <DerpUsageCard devices={devices.data ?? []} nodes={allNodes} regions={regions} loading={loading || (devices.isPending && !devices.data)} />
          </div>
          <div className="sm:col-span-2 xl:col-span-1">
            <SlowestLinksCard nodes={allNodes} threshold={latencyRule?.threshold} loading={loading} />
          </div>
        </div>
      </div>
    </>
  )
}
