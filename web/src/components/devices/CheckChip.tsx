import type { ReactNode } from 'react'
import type { LucideIcon } from 'lucide-react'
import { Check } from 'lucide-react'
import { cn } from '../../lib/cn'

export interface CheckChipProps {
  checked: boolean
  onCheckedChange: (checked: boolean) => void
  children: ReactNode
  icon?: LucideIcon
  /** Facet count shown after the label. */
  count?: number
  disabled?: boolean
  className?: string
}

/** Toggle pill backed by a real checkbox (keyboard + screen-reader friendly). */
export function CheckChip({ checked, onCheckedChange, children, icon: Icon, count, disabled, className }: CheckChipProps) {
  return (
    <label
      className={cn(
        'inline-flex h-7 max-w-full cursor-pointer select-none items-center gap-1.5 rounded-md border px-2 text-xs font-medium transition-colors',
        'has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-offset-2 has-[:focus-visible]:outline-ring',
        checked ? 'border-accent/40 bg-accent-soft text-accent-text' : 'border-border bg-surface text-fg-secondary hover:border-border-strong hover:text-fg',
        disabled && 'cursor-not-allowed opacity-50',
        className,
      )}
    >
      <input type="checkbox" className="sr-only" checked={checked} disabled={disabled} onChange={(e) => onCheckedChange(e.target.checked)} />
      {checked ? <Check className="size-3.5 shrink-0" aria-hidden="true" /> : Icon ? <Icon className="size-3.5 shrink-0" aria-hidden="true" /> : null}
      <span className="truncate">{children}</span>
      {count !== undefined ? <span className={cn('num text-[11px]', checked ? 'text-accent-text/80' : 'text-fg-muted')}>{count}</span> : null}
    </label>
  )
}
