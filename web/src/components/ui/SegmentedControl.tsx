import { useRef, type KeyboardEvent, type ReactNode } from 'react'
import type { LucideIcon } from 'lucide-react'
import { cn } from '../../lib/cn'

export interface SegmentOption<T extends string = string> {
  value: T
  label: ReactNode
  icon?: LucideIcon
  /** Accessible name when label is icon-only. */
  ariaLabel?: string
  disabled?: boolean
}

export interface SegmentedControlProps<T extends string = string> {
  options: ReadonlyArray<SegmentOption<T>>
  value: T
  onValueChange: (v: T) => void
  size?: 'xs' | 'sm' | 'md'
  'aria-label': string
  className?: string
  block?: boolean
}

const SIZES = { xs: 'h-6 px-2 text-[11px]', sm: 'h-7 px-2.5 text-xs', md: 'h-8 px-3 text-[13px]' }

/** Radio-group styled as a segmented control. Arrow keys move selection. */
export function SegmentedControl<T extends string = string>({ options, value, onValueChange, size = 'sm', className, block, ...aria }: SegmentedControlProps<T>) {
  const ref = useRef<HTMLDivElement>(null)
  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    const enabled = options.filter((o) => !o.disabled)
    const idx = enabled.findIndex((o) => o.value === value)
    let next = idx
    if (e.key === 'ArrowRight' || e.key === 'ArrowDown') next = (idx + 1) % enabled.length
    else if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') next = (idx - 1 + enabled.length) % enabled.length
    else if (e.key === 'Home') next = 0
    else if (e.key === 'End') next = enabled.length - 1
    else return
    e.preventDefault()
    const o = enabled[next]
    if (!o) return
    onValueChange(o.value)
    ref.current?.querySelector<HTMLElement>(`[data-value="${CSS.escape(o.value)}"]`)?.focus()
  }
  return (
    <div
      ref={ref}
      role="radiogroup"
      aria-label={aria['aria-label']}
      onKeyDown={onKeyDown}
      className={cn('inline-flex items-center gap-0.5 rounded-md border border-border bg-surface-inset p-0.5', block && 'flex w-full', className)}
    >
      {options.map((o) => {
        const selected = o.value === value
        const Icon = o.icon
        return (
          <button
            key={o.value}
            type="button"
            role="radio"
            aria-checked={selected}
            aria-label={o.ariaLabel}
            data-value={o.value}
            tabIndex={selected ? 0 : -1}
            disabled={o.disabled}
            onClick={() => onValueChange(o.value)}
            className={cn(
              'inline-flex items-center justify-center gap-1.5 whitespace-nowrap rounded-[5px] font-medium transition-colors focus-ring-inset disabled:opacity-50',
              SIZES[size],
              block && 'flex-1',
              selected ? 'bg-surface-raised text-fg shadow-xs' : 'text-fg-muted hover:text-fg',
            )}
          >
            {Icon ? <Icon className={size === 'xs' ? 'size-3' : 'size-3.5'} aria-hidden="true" /> : null}
            {o.label}
          </button>
        )
      })}
    </div>
  )
}
