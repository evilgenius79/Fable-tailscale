import { useMemo } from 'react'
import { BarChart3, Radio, Timer, Waypoints } from 'lucide-react'
import type { Device, TopologyNode } from '../../api/types'
import { formatInt, formatLatency, plural } from '../../lib/format'
import { BarList, DonutChart } from '../charts'
import { Badge } from '../ui/Badge'
import { Card, CardHeader } from '../ui/Card'
import { EmptyState } from '../ui/EmptyState'
import { Meter } from '../ui/ProgressBar'
import { Skeleton } from '../ui/Skeleton'
import { aggregateDerp, latencyHistogram, latencyStats, pathSummary, slowestLinks } from './topology'

// ---------------------------------------------------------------------------
// DERP relay usage
// ---------------------------------------------------------------------------

export function DerpUsageCard({ devices, nodes, regions, loading }: { devices: ReadonlyArray<Device>; nodes: ReadonlyArray<TopologyNode>; regions: Record<string, string>; loading?: boolean }) {
  const stats = useMemo(() => aggregateDerp(devices, nodes, regions), [devices, nodes, regions])
  const inUse = stats.filter((s) => s.inUse > 0)
  const maxLatency = Math.max(0, ...stats.map((s) => s.avgLatencyMs ?? 0))
  return (
    <Card>
      <CardHeader
        title="DERP relays"
        description={loading ? 'Loading…' : inUse.length ? `${plural(inUse.length, 'region')} carrying traffic · ${plural(inUse.reduce((a, s) => a + s.inUse, 0), 'relayed device')}` : 'No device is relayed right now'}
        icon={Radio}
      />
      {loading ? (
        <div className="space-y-3">
          {Array.from({ length: 3 }, (_, i) => (
            <Skeleton key={i} height={28} />
          ))}
        </div>
      ) : stats.length ? (
        <ul className="divide-y divide-border-subtle" aria-label="DERP regions">
          {stats.map((s) => (
            <li key={s.code} className="py-2.5 first:pt-0 last:pb-0">
              <div className="flex items-center justify-between gap-3 text-[13px]">
                <span className="flex min-w-0 items-center gap-2">
                  <span className="w-8 shrink-0 font-mono text-xs font-semibold uppercase text-fg">{s.code}</span>
                  <span className="truncate text-fg-secondary">{s.name !== s.code ? s.name : ''}</span>
                </span>
                <span className="shrink-0 num text-fg" title="Average probe latency across devices">
                  {formatLatency(s.avgLatencyMs)}
                </span>
              </div>
              <div className="mt-1.5 flex items-center gap-3">
                <Meter value={s.avgLatencyMs} max={maxLatency || 1} tone={s.inUse ? 'relay' : 'neutral'} size="xs" label={`${s.code} average latency relative to the slowest region`} className="min-w-0 flex-1" />
                <span className="flex shrink-0 items-center gap-1.5 text-[11px] text-fg-muted num">
                  {s.inUse ? (
                    <Badge size="sm" tone="relay" dot>
                      {plural(s.inUse, 'device')} relayed
                    </Badge>
                  ) : (
                    <span>not in use</span>
                  )}
                  {s.preferred ? <span title="Devices whose home region this is">· home for {formatInt(s.preferred)}</span> : null}
                </span>
              </div>
            </li>
          ))}
        </ul>
      ) : (
        <EmptyState size="sm" bordered={false} icon={Radio} title="No DERP data yet" description="Relay regions appear once devices report connectivity probes." />
      )}
    </Card>
  )
}

// ---------------------------------------------------------------------------
// Path summary
// ---------------------------------------------------------------------------

export function PathSummaryCard({ nodes, loading }: { nodes: ReadonlyArray<TopologyNode>; loading?: boolean }) {
  const s = useMemo(() => pathSummary(nodes), [nodes])
  const data = [
    { label: 'Direct', value: s.direct, tone: 'direct' as const },
    { label: 'Relayed', value: s.relay, tone: 'relay' as const },
    { label: 'No path yet', value: s.unknown, tone: 'info' as const },
    { label: 'Offline', value: s.offline, tone: 'offline' as const },
  ]
  const directShare = s.online ? Math.round((s.direct / s.online) * 100) : null
  return (
    <Card>
      <CardHeader title="Paths" description={loading ? 'Loading…' : directShare !== null ? `${directShare}% of online devices connect directly` : 'How peers reach the hub'} icon={Waypoints} />
      {loading ? (
        <div className="flex items-center gap-5">
          <Skeleton width={120} height={120} rounded="full" />
          <div className="flex-1 space-y-2">
            <Skeleton height={12} />
            <Skeleton height={12} width="80%" />
            <Skeleton height={12} width="60%" />
          </div>
        </div>
      ) : (
        <DonutChart data={data} size={120} thickness={12} centerValue={formatInt(s.online)} centerLabel="online" ariaLabel={`Paths: ${s.direct} direct, ${s.relay} relayed, ${s.unknown} unknown, ${s.offline} offline`} />
      )}
    </Card>
  )
}

// ---------------------------------------------------------------------------
// Latency distribution
// ---------------------------------------------------------------------------

export function LatencyDistributionCard({ nodes, loading }: { nodes: ReadonlyArray<TopologyNode>; loading?: boolean }) {
  const values = useMemo(() => nodes.filter((n) => n.online && !n.isSelf).map((n) => n.latencyMs), [nodes])
  const buckets = useMemo(() => latencyHistogram(values), [values])
  const stats = useMemo(() => latencyStats(values), [values])
  return (
    <Card>
      <CardHeader title="Latency distribution" description={loading ? 'Loading…' : stats.count ? `${plural(stats.count, 'online peer')} with a ping` : 'Ping round-trip from the hub'} icon={BarChart3} />
      {loading ? (
        <div className="space-y-3">
          {Array.from({ length: 4 }, (_, i) => (
            <Skeleton key={i} height={22} />
          ))}
        </div>
      ) : stats.count ? (
        <>
          <dl className="mb-4 grid grid-cols-3 gap-3 text-xs">
            {[
              ['Median', stats.p50],
              ['p95', stats.p95],
              ['Max', stats.max],
            ].map(([label, v]) => (
              <div key={label as string}>
                <dt className="text-fg-muted">{label}</dt>
                <dd className="text-base font-semibold text-fg">{formatLatency(v as number | null)}</dd>
              </div>
            ))}
          </dl>
          <BarList items={buckets.map((b) => ({ key: b.label, label: b.label, value: b.count }))} sort={false} format={(v) => plural(v, 'device')} ariaLabel="Devices per latency bucket" />
        </>
      ) : (
        <EmptyState size="sm" bordered={false} icon={Timer} title="No latency samples" description="Pings run every 30 seconds for online peers." />
      )}
    </Card>
  )
}

// ---------------------------------------------------------------------------
// Slowest links
// ---------------------------------------------------------------------------

export function SlowestLinksCard({ nodes, threshold, loading, limit = 6 }: { nodes: ReadonlyArray<TopologyNode>; threshold?: number; loading?: boolean; limit?: number }) {
  const slow = useMemo(() => slowestLinks(nodes, limit), [nodes, limit])
  return (
    <Card>
      <CardHeader title="Slowest links" description={loading ? 'Loading…' : threshold ? `Highlighted above the ${formatLatency(threshold)} alert threshold` : 'Highest round-trip from the hub'} icon={Timer} />
      {loading ? (
        <div className="space-y-3">
          {Array.from({ length: 4 }, (_, i) => (
            <Skeleton key={i} height={22} />
          ))}
        </div>
      ) : (
        <BarList
          items={slow.map((n) => ({
            key: n.id,
            label: n.name,
            sublabel: n.path === 'relay' ? `via ${n.relay ?? 'relay'}` : 'direct',
            value: n.latencyMs ?? 0,
            to: `/devices/${encodeURIComponent(n.id)}`,
            tone: threshold && (n.latencyMs ?? 0) >= threshold ? ('warning' as const) : undefined,
          }))}
          format={formatLatency}
          sort={false}
          emptyMessage="No online peers with a ping yet"
          ariaLabel="Slowest links by latency"
        />
      )}
    </Card>
  )
}
