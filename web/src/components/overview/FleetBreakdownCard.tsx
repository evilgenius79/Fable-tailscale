import { useMemo } from 'react'
import type { Overview } from '../../api/types'
import { formatInt } from '../../lib/format'
import { BarList } from '../charts/BarList'
import { DonutChart } from '../charts/DonutChart'
import { Card, CardHeader } from '../ui/Card'
import { Skeleton } from '../ui/Skeleton'
import { osSlices, userItems } from './overview'

export interface FleetBreakdownCardProps {
  overview: Overview | undefined
  loading: boolean
}

/** Part-to-whole by OS (donut, ≤6 slices) beside ranked device counts per user (bar list). */
export function FleetBreakdownCard({ overview: o, loading }: FleetBreakdownCardProps) {
  const os = useMemo(() => osSlices(o?.osBreakdown), [o?.osBreakdown])
  const users = useMemo(() => userItems(o?.userBreakdown), [o?.userBreakdown])
  return (
    <Card padding="none">
      <CardHeader divider title="Fleet breakdown" description="Devices by operating system and owner" />
      <div className="grid sm:grid-cols-2 sm:divide-x sm:divide-border">
        <div className="px-4 py-4 sm:px-5">
          <h4 className="mb-3 text-[11px] font-semibold uppercase tracking-wider text-fg-muted">By operating system</h4>
          {loading && !o ? (
            <div className="flex items-center gap-5" aria-busy="true" aria-label="Loading breakdown">
              <Skeleton width={120} height={120} rounded="full" />
              <div className="flex-1 space-y-2">
                <Skeleton height={10} />
                <Skeleton height={10} width="80%" />
                <Skeleton height={10} width="60%" />
              </div>
            </div>
          ) : (
            <DonutChart
              data={os}
              size={120}
              thickness={12}
              centerLabel="devices"
              centerValue={o ? formatInt(o.devices) : '—'}
              ariaLabel={`Devices by operating system: ${os.map((s) => `${s.label} ${s.value}`).join(', ')}`}
            />
          )}
        </div>
        <div className="border-t border-border px-4 py-4 sm:border-t-0 sm:px-5">
          <h4 className="mb-3 text-[11px] font-semibold uppercase tracking-wider text-fg-muted">By user</h4>
          {loading && !o ? (
            <div className="space-y-3" aria-hidden="true">
              <Skeleton height={10} width="70%" />
              <Skeleton height={10} width="55%" />
              <Skeleton height={10} width="40%" />
            </div>
          ) : (
            <BarList
              items={users.map((u) => ({ key: u.label, label: u.label, value: u.value, to: `/devices?user=${encodeURIComponent(u.label)}` }))}
              limit={6}
              format={formatInt}
              emptyMessage="No devices yet"
              ariaLabel="Devices per user"
            />
          )}
        </div>
      </div>
    </Card>
  )
}
