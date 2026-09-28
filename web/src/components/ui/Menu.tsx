import {
  cloneElement,
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type KeyboardEvent as ReactKeyboardEvent,
  type MouseEvent as ReactMouseEvent,
  type ReactElement,
  type ReactNode,
} from 'react'
import { createPortal } from 'react-dom'
import { Link } from 'react-router-dom'
import { Check } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { cn } from '../../lib/cn'

export interface MenuItem {
  id: string
  label: ReactNode
  icon?: LucideIcon
  /** Secondary text at the right edge (shortcut, value). */
  hint?: ReactNode
  onSelect?: () => void
  /** Render as a router link. */
  to?: string
  danger?: boolean
  disabled?: boolean
  /** Renders as menuitemcheckbox with a check mark. */
  checked?: boolean
  separatorBefore?: boolean
}

export interface DropdownMenuProps {
  /** A button-like element; receives onClick/aria-haspopup/aria-expanded. */
  trigger: ReactElement<Record<string, unknown>>
  items: MenuItem[]
  align?: 'start' | 'end'
  side?: 'bottom' | 'top'
  /** Accessible name for the menu. */
  label?: string
  /** Optional header content (identity card, title). */
  header?: ReactNode
  footer?: ReactNode
  width?: number
  onOpenChange?: (open: boolean) => void
  className?: string
}

const GAP = 6

/** Portal dropdown menu with roving focus, typeahead-free keyboard nav, outside-click and Escape close. */
export function DropdownMenu({ trigger, items, align = 'start', side = 'bottom', label, header, footer, width = 224, onOpenChange, className }: DropdownMenuProps) {
  const id = useId()
  const [open, setOpen] = useState(false)
  const [pos, setPos] = useState<{ top: number; left: number } | null>(null)
  const triggerRef = useRef<HTMLElement | null>(null)
  const menuRef = useRef<HTMLDivElement | null>(null)

  const setOpenState = useCallback(
    (v: boolean) => {
      setOpen(v)
      onOpenChange?.(v)
    },
    [onOpenChange],
  )

  useLayoutEffect(() => {
    if (!open || !triggerRef.current || !menuRef.current) return
    const r = triggerRef.current.getBoundingClientRect()
    const m = menuRef.current.getBoundingClientRect()
    const vw = window.innerWidth
    const vh = window.innerHeight
    let left = align === 'end' ? r.right - m.width : r.left
    left = Math.max(8, Math.min(left, vw - m.width - 8))
    let top = side === 'top' ? r.top - GAP - m.height : r.bottom + GAP
    if (top + m.height > vh - 8) top = Math.max(8, r.top - GAP - m.height)
    setPos({ top, left })
  }, [open, align, side])

  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent | TouchEvent) => {
      const t = e.target as Node
      if (menuRef.current?.contains(t) || triggerRef.current?.contains(t)) return
      setOpenState(false)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault()
        setOpenState(false)
        triggerRef.current?.focus()
      }
    }
    const onScroll = () => setOpenState(false)
    document.addEventListener('mousedown', onDown)
    document.addEventListener('touchstart', onDown)
    document.addEventListener('keydown', onKey)
    window.addEventListener('resize', onScroll)
    return () => {
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('touchstart', onDown)
      document.removeEventListener('keydown', onKey)
      window.removeEventListener('resize', onScroll)
    }
  }, [open, setOpenState])

  useEffect(() => {
    if (!open) return
    const first = menuRef.current?.querySelector<HTMLElement>('[role^="menuitem"]:not([aria-disabled="true"])')
    first?.focus()
  }, [open, pos])

  const focusables = () => Array.from(menuRef.current?.querySelectorAll<HTMLElement>('[role^="menuitem"]:not([aria-disabled="true"])') ?? [])

  const onMenuKeyDown = (e: ReactKeyboardEvent<HTMLDivElement>) => {
    const list = focusables()
    const idx = list.indexOf(document.activeElement as HTMLElement)
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      list[(idx + 1) % list.length]?.focus()
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      list[(idx - 1 + list.length) % list.length]?.focus()
    } else if (e.key === 'Home') {
      e.preventDefault()
      list[0]?.focus()
    } else if (e.key === 'End') {
      e.preventDefault()
      list[list.length - 1]?.focus()
    } else if (e.key === 'Tab') {
      setOpenState(false)
    }
  }

  const select = (item: MenuItem) => {
    if (item.disabled) return
    setOpenState(false)
    item.onSelect?.()
    triggerRef.current?.focus()
  }

  const triggerProps = trigger.props
  const clonedTrigger = cloneElement(trigger, {
    ref: (el: HTMLElement | null) => {
      triggerRef.current = el
      const r = (triggerProps as { ref?: unknown }).ref
      if (typeof r === 'function') r(el)
      else if (r && typeof r === 'object') (r as { current: HTMLElement | null }).current = el
    },
    'aria-haspopup': 'menu',
    'aria-expanded': open,
    'aria-controls': open ? id : undefined,
    onClick: (e: ReactMouseEvent) => {
      ;(triggerProps.onClick as ((e: ReactMouseEvent) => void) | undefined)?.(e)
      setOpenState(!open)
    },
    onKeyDown: (e: ReactKeyboardEvent) => {
      ;(triggerProps.onKeyDown as ((e: ReactKeyboardEvent) => void) | undefined)?.(e)
      if (e.key === 'ArrowDown' && !open) {
        e.preventDefault()
        setOpenState(true)
      }
    },
  })

  return (
    <>
      {clonedTrigger}
      {open
        ? createPortal(
            <div
              ref={menuRef}
              id={id}
              role="menu"
              aria-label={label}
              onKeyDown={onMenuKeyDown}
              style={{ top: pos?.top ?? -9999, left: pos?.left ?? -9999, width }}
              className={cn(
                'fixed z-[105] max-w-[calc(100vw-16px)] overflow-hidden rounded-lg border border-border bg-surface-raised p-1 shadow-overlay',
                pos ? 'animate-scale-in' : 'opacity-0',
                className,
              )}
            >
              {header ? <div className="border-b border-border px-2 py-2 mb-1">{header}</div> : null}
              {items.map((item) => {
                const Icon = item.icon
                const cls = cn(
                  'flex w-full cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-left text-[13px] outline-none transition-colors',
                  item.danger ? 'text-critical hover:bg-critical-soft focus:bg-critical-soft' : 'text-fg hover:bg-surface-hover focus:bg-surface-hover',
                  item.disabled && 'cursor-not-allowed opacity-50 hover:bg-transparent',
                )
                const inner = (
                  <>
                    {item.checked !== undefined ? (
                      <span className="flex size-4 items-center justify-center">{item.checked ? <Check className="size-4" aria-hidden="true" /> : null}</span>
                    ) : Icon ? (
                      <Icon className={cn('size-4', item.danger ? 'text-critical' : 'text-fg-muted')} aria-hidden="true" />
                    ) : null}
                    <span className="min-w-0 flex-1 truncate">{item.label}</span>
                    {item.hint ? <span className="ml-auto shrink-0 text-xs text-fg-muted">{item.hint}</span> : null}
                  </>
                )
                return (
                  <div key={item.id}>
                    {item.separatorBefore ? <div role="separator" className="my-1 h-px bg-border" /> : null}
                    {item.to && !item.disabled ? (
                      <Link to={item.to} role="menuitem" tabIndex={-1} className={cls} onClick={() => select({ ...item, onSelect: item.onSelect })}>
                        {inner}
                      </Link>
                    ) : (
                      <button
                        type="button"
                        role={item.checked !== undefined ? 'menuitemcheckbox' : 'menuitem'}
                        aria-checked={item.checked}
                        aria-disabled={item.disabled || undefined}
                        tabIndex={-1}
                        className={cls}
                        onClick={() => select(item)}
                      >
                        {inner}
                      </button>
                    )}
                  </div>
                )
              })}
              {footer ? <div className="mt-1 border-t border-border px-2 py-1.5">{footer}</div> : null}
            </div>,
            document.body,
          )
        : null}
    </>
  )
}
