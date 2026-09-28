import { useMemo } from 'react'
import { ArrowDown, ArrowUp } from 'lucide-react'
import type { Overview } from '../../api/types'
import { formatBitrate, formatDuration } from '../../lib/format'
import { TimeSeriesChart } from '../charts/TimeSeriesChart'
import { Card, CardHeader } from '../ui/Card'
import { sparklineWindow } from './overview'

export interface ThroughputChartProps {
  overview: Overview | undefined
  loading: boolean
  fetching: boolean
  error?: unknown
  onRetry?: () => void
}

/** Fleet-wide Tailscale receive/transmit rate over the overview sparkline window. */
export function ThroughputChart({ overview: o, loading, fetching, error, onRetry }: ThroughputChartProps) {
  const s = o?.sparklines
  const w = sparklineWindow(s)
  const data = useMemo(() => (s ? s.t.map((t, i) => ({ t, rx: s.rxRate[i] ?? null, tx: s.txRate[i] ?? null })) : []), [s])
  const description = w.span ? `Last ${formatDuration(w.span, 1)}${w.step ? ` · ${formatDuration(w.step, 1)} resolution` : ''}` : 'Fleet-wide Tailscale traffic'
  return (
    <Card padding="none">
      <CardHeader
        divider
        title="Network throughput"
        description={description}
        actions={
          o ? (
            <span className="num flex items-center gap-2.5 text-xs text-fg-secondary">
              <span className="inline-flex items-center gap-1 whitespace-nowrap">
                <ArrowDown className="size-3 text-fg-faint" aria-hidden="true" />
                <span className="sr-only">Receive </span>
                {formatBitrate(o.totalRxRate)}
              </span>
              <span className="inline-flex items-center gap-1 whitespace-nowrap">
                <ArrowUp className="size-3 text-fg-faint" aria-hidden="true" />
                <span className="sr-only">Transmit </span>
                {formatBitrate(o.totalTxRate)}
              </span>
            </span>
          ) : null
        }
      />
      <div className="px-3 pb-3 pt-4 sm:px-5 sm:pb-4">
        <TimeSeriesChart
          data={data}
          series={[
            { key: 'rx', label: 'Receive', kind: 'area' },
            { key: 'tx', label: 'Transmit', kind: 'area' },
          ]}
          range={w.range}
          height={236}
          yFormat={formatBitrate}
          loading={loading}
          fetching={fetching}
          error={error}
          onRetry={onRetry}
          emptyMessage="No throughput history yet"
          ariaLabel="Network throughput over time"
        />
      </div>
    </Card>
  )
}
