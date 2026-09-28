import { useRef, type KeyboardEvent, type ReactNode } from 'react'
import type { LucideIcon } from 'lucide-react'
import { cn } from '../../lib/cn'
import { formatCompact } from '../../lib/format'
import { useScrollFade } from '../../lib/useScrollFade'

export interface TabItem<T extends string = string> {
  id: T
  label: ReactNode
  icon?: LucideIcon
  count?: number
  disabled?: boolean
}

export interface TabsProps<T extends string = string> {
  tabs: ReadonlyArray<TabItem<T>>
  value: T
  onValueChange: (id: T) => void
  variant?: 'underline' | 'pills'
  size?: 'sm' | 'md'
  'aria-label'?: string
  className?: string
  /** id prefix used for aria-controls (matches <TabPanel id>). */
  idPrefix?: string
}

/** Roving-tabindex tablist; arrow keys move, Home/End jump. */
export function Tabs<T extends string = string>({ tabs, value, onValueChange, variant = 'underline', size = 'md', className, idPrefix = 'tab', ...aria }: TabsProps<T>) {
  const listRef = useRef<HTMLDivElement>(null)
  // The scrollbar is hidden, so fade the clipped edge to show there is more to scroll to.
  const fade = useScrollFade(listRef)
  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    const enabled = tabs.filter((t) => !t.disabled)
    const idx = enabled.findIndex((t) => t.id === value)
    let next = idx
    if (e.key === 'ArrowRight') next = (idx + 1) % enabled.length
    else if (e.key === 'ArrowLeft') next = (idx - 1 + enabled.length) % enabled.length
    else if (e.key === 'Home') next = 0
    else if (e.key === 'End') next = enabled.length - 1
    else return
    e.preventDefault()
    const t = enabled[next]
    if (!t) return
    onValueChange(t.id)
    listRef.current?.querySelector<HTMLElement>(`[data-tab-id="${CSS.escape(t.id)}"]`)?.focus()
  }
  return (
    <div
      ref={listRef}
      role="tablist"
      aria-label={aria['aria-label']}
      onKeyDown={onKeyDown}
      style={fade}
      className={cn(
        'flex max-w-full items-center overflow-x-auto scrollbar-none',
        variant === 'underline' ? 'gap-1 border-b border-border' : 'gap-1 rounded-lg bg-surface-inset p-1',
        className,
      )}
    >
      {tabs.map((t) => {
        const selected = t.id === value
        const Icon = t.icon
        return (
          <button
            key={t.id}
            type="button"
            role="tab"
            id={`${idPrefix}-${t.id}`}
            aria-controls={`${idPrefix}-${t.id}-panel`}
            aria-selected={selected}
            data-tab-id={t.id}
            tabIndex={selected ? 0 : -1}
            disabled={t.disabled}
            onClick={() => onValueChange(t.id)}
            className={cn(
              'relative inline-flex shrink-0 items-center gap-1.5 whitespace-nowrap font-medium transition-colors focus-ring disabled:opacity-50',
              size === 'sm' ? 'text-[13px]' : 'text-sm',
              variant === 'underline' &&
                cn(
                  '-mb-px border-b-2 px-1 pb-2.5 pt-1.5',
                  selected ? 'border-fg text-fg' : 'border-transparent text-fg-muted hover:border-border-strong hover:text-fg',
                ),
              variant === 'pills' &&
                cn('rounded-md px-3 py-1.5', selected ? 'bg-surface-raised text-fg shadow-xs' : 'text-fg-muted hover:text-fg'),
            )}
          >
            {Icon ? <Icon className="size-4" aria-hidden="true" /> : null}
            {t.label}
            {typeof t.count === 'number' && t.count > 0 ? (
              <span className={cn('rounded-md px-1.5 py-0.5 text-[11px] leading-none num', selected ? 'bg-accent-soft text-accent-text' : 'bg-surface-inset text-fg-muted')}>
                {formatCompact(t.count)}
              </span>
            ) : null}
          </button>
        )
      })}
    </div>
  )
}

export function TabPanel({ id, active, idPrefix = 'tab', className, children }: { id: string; active: boolean; idPrefix?: string; className?: string; children: ReactNode }) {
  if (!active) return null
  return (
    <div role="tabpanel" id={`${idPrefix}-${id}-panel`} aria-labelledby={`${idPrefix}-${id}`} tabIndex={0} className={cn('focus-ring rounded-md', className)}>
      {children}
    </div>
  )
}
