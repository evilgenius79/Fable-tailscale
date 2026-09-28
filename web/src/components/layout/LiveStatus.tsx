import { cn } from '../../lib/cn'
import { formatRelative } from '../../lib/format'
import type { StatusTone } from '../../lib/status'
import { useUIStore, selectLive, type LiveState } from '../../store'
import { StatusDot } from '../ui/StatusDot'
import { Tooltip } from '../ui/Tooltip'

const META: Record<LiveState, { label: string; tone: StatusTone; detail: string; pulse: boolean }> = {
  connecting: { label: 'Connecting', tone: 'neutral', detail: 'Opening the live event stream…', pulse: false },
  connected: { label: 'Live', tone: 'online', detail: 'Receiving live updates from the hub.', pulse: true },
  reconnecting: { label: 'Reconnecting', tone: 'warning', detail: 'Live stream interrupted; retrying automatically.', pulse: true },
  polling: { label: 'Polling', tone: 'warning', detail: 'Live stream unavailable; refreshing every 15 seconds instead.', pulse: false },
}

/** Live-connection pill for the top bar. Text label hides below sm; the dot and tooltip stay. */
export function LiveStatusIndicator({ className }: { className?: string }) {
  const live = useUIStore(selectLive)
  const m = META[live.state]
  const tip = (
    <div className="space-y-0.5">
      <p>{m.detail}</p>
      <p className="text-fg-muted">
        Last update: {live.lastTick ? formatRelative(live.lastTick) : 'none yet'}
        {live.attempts ? ` · ${live.attempts} retr${live.attempts === 1 ? 'y' : 'ies'}` : ''}
      </p>
    </div>
  )
  return (
    <Tooltip content={tip} side="bottom">
      <div
        role="status"
        aria-live="polite"
        className={cn(
          'inline-flex h-8 items-center gap-2 rounded-md px-2 text-xs font-medium text-fg-secondary sm:border sm:border-border sm:bg-surface sm:px-2.5',
          className,
        )}
      >
        <StatusDot tone={m.tone} pulse={m.pulse} size="sm" />
        <span className="hidden sm:inline">{m.label}</span>
        <span className="sr-only sm:hidden">{m.label}</span>
      </div>
    </Tooltip>
  )
}
