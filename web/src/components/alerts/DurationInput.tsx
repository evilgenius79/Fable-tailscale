import { useId } from 'react'
import { cn } from '../../lib/cn'
import { inputBase } from '../ui/Input'
import { formatForSeconds, joinSeconds } from './alerts'

export interface DurationInputProps {
  minutes: string
  seconds: string
  onChange: (next: { minutes: string; seconds: string }) => void
  disabled?: boolean
  invalid?: boolean
  /** id of the minutes input (for <label htmlFor>). */
  id?: string
  describedBy?: string
}

/** Minutes + seconds pair (seconds precision) with a live "= 5m 30s" readout. */
export function DurationInput({ minutes, seconds, onChange, disabled, invalid, id, describedBy }: DurationInputProps) {
  const gen = useId()
  const minId = id ?? `${gen}-min`
  const secId = `${gen}-sec`
  const total = joinSeconds(Number(minutes), Number(seconds))
  const field = cn(inputBase, 'h-9 pl-3 pr-2 text-sm num', 'w-[88px]')
  return (
    <div className="flex flex-wrap items-center gap-2">
      <div className="relative flex items-center">
        <input
          id={minId}
          type="number"
          inputMode="numeric"
          min={0}
          step={1}
          value={minutes}
          disabled={disabled}
          aria-invalid={invalid || undefined}
          aria-describedby={describedBy}
          aria-label="Minutes"
          onChange={(e) => onChange({ minutes: e.target.value, seconds })}
          className={field}
        />
        <span className="pointer-events-none absolute right-2.5 text-xs text-fg-muted" aria-hidden="true">
          min
        </span>
      </div>
      <div className="relative flex items-center">
        <input
          id={secId}
          type="number"
          inputMode="numeric"
          min={0}
          max={59}
          step={1}
          value={seconds}
          disabled={disabled}
          aria-invalid={invalid || undefined}
          aria-describedby={describedBy}
          aria-label="Seconds"
          onChange={(e) => onChange({ minutes, seconds: e.target.value })}
          className={field}
        />
        <span className="pointer-events-none absolute right-2.5 text-xs text-fg-muted" aria-hidden="true">
          sec
        </span>
      </div>
      <span className="num text-xs text-fg-muted" aria-live="polite">
        = {formatForSeconds(total)}
        {total > 0 ? <span className="text-fg-faint"> ({total}s)</span> : null}
      </span>
    </div>
  )
}
