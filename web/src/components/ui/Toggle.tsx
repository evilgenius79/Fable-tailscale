import { useId, type ReactNode } from 'react'
import { cn } from '../../lib/cn'

export interface ToggleProps {
  checked: boolean
  onCheckedChange: (checked: boolean) => void
  label?: ReactNode
  description?: ReactNode
  size?: 'sm' | 'md'
  disabled?: boolean
  id?: string
  className?: string
  /** Put the label before the switch (default) or after. */
  labelPosition?: 'start' | 'end'
  /** Accessible name when there is no visible `label` (table cells, icon-only rows). */
  'aria-label'?: string
}

/** Accessible switch (role="switch"). */
export function Toggle({ checked, onCheckedChange, label, description, size = 'md', disabled, id, className, labelPosition = 'start', 'aria-label': ariaLabel }: ToggleProps) {
  const gen = useId()
  const switchId = id ?? gen
  const labelId = `${switchId}-label`
  const descId = `${switchId}-desc`
  const dims = size === 'sm' ? { track: 'h-4 w-7', knob: 'size-3', on: 'translate-x-3' } : { track: 'h-5 w-9', knob: 'size-4', on: 'translate-x-4' }
  const sw = (
    <button
      id={switchId}
      type="button"
      role="switch"
      aria-checked={checked}
      aria-labelledby={label ? labelId : undefined}
      aria-label={!label ? ariaLabel : undefined}
      aria-describedby={description ? descId : undefined}
      disabled={disabled}
      onClick={() => onCheckedChange(!checked)}
      className={cn(
        'relative inline-flex shrink-0 items-center rounded-full border border-transparent p-0.5 transition-colors focus-ring disabled:opacity-50',
        dims.track,
        checked ? 'bg-accent' : 'bg-border-strong',
      )}
    >
      <span aria-hidden="true" className={cn('block rounded-full bg-white shadow-sm transition-transform', dims.knob, checked ? dims.on : 'translate-x-0')} />
    </button>
  )
  if (!label) return <span className={className}>{sw}</span>
  return (
    <div className={cn('flex items-start justify-between gap-4', labelPosition === 'end' && 'flex-row-reverse justify-end', className)}>
      <div className="min-w-0">
        <label id={labelId} htmlFor={switchId} className="block cursor-pointer text-[13px] font-medium text-fg">
          {label}
        </label>
        {description ? (
          <p id={descId} className="text-xs leading-5 text-fg-muted">
            {description}
          </p>
        ) : null}
      </div>
      {sw}
    </div>
  )
}
