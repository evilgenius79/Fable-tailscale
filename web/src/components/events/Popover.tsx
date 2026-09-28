import {
  cloneElement,
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type FocusEvent as ReactFocusEvent,
  type KeyboardEvent as ReactKeyboardEvent,
  type MouseEvent as ReactMouseEvent,
  type ReactElement,
  type ReactNode,
} from 'react'
import { createPortal } from 'react-dom'
import { cn } from '../../lib/cn'

export interface PopoverProps {
  /** A button-like element; receives onClick/aria-haspopup/aria-expanded. */
  trigger: ReactElement<Record<string, unknown>>
  /** Panel content; a function receives `close`. */
  children: ReactNode | ((close: () => void) => ReactNode)
  /** Accessible name for the panel. */
  label: string
  align?: 'start' | 'end'
  width?: number
  onOpenChange?: (open: boolean) => void
  className?: string
}

const GAP = 6
const FOCUSABLE = 'a[href],button:not([disabled]),input:not([disabled]),select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"])'

/**
 * Non-modal anchored panel for multi-select filters (stays open while
 * toggling options, unlike DropdownMenu). Closes on Escape, outside click,
 * focus leaving the panel, or viewport resize; focus returns to the trigger.
 */
export function Popover({ trigger, children, label, align = 'start', width = 320, onOpenChange, className }: PopoverProps) {
  const id = useId()
  const [open, setOpen] = useState(false)
  const [pos, setPos] = useState<{ top: number; left: number } | null>(null)
  const triggerRef = useRef<HTMLElement | null>(null)
  const panelRef = useRef<HTMLDivElement | null>(null)

  const setOpenState = useCallback(
    (v: boolean) => {
      setOpen(v)
      onOpenChange?.(v)
    },
    [onOpenChange],
  )
  const close = useCallback(() => {
    setOpenState(false)
    triggerRef.current?.focus()
  }, [setOpenState])

  useLayoutEffect(() => {
    if (!open || !triggerRef.current || !panelRef.current) return
    const r = triggerRef.current.getBoundingClientRect()
    const m = panelRef.current.getBoundingClientRect()
    const vw = window.innerWidth
    const vh = window.innerHeight
    let left = align === 'end' ? r.right - m.width : r.left
    left = Math.max(8, Math.min(left, vw - m.width - 8))
    let top = r.bottom + GAP
    if (top + m.height > vh - 8) top = Math.max(8, r.top - GAP - m.height)
    setPos({ top, left })
  }, [open, align])

  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent | TouchEvent) => {
      const t = e.target as Node
      if (panelRef.current?.contains(t) || triggerRef.current?.contains(t)) return
      setOpenState(false)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault()
        close()
      }
    }
    const onResize = () => setOpenState(false)
    document.addEventListener('mousedown', onDown)
    document.addEventListener('touchstart', onDown)
    document.addEventListener('keydown', onKey)
    window.addEventListener('resize', onResize)
    return () => {
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('touchstart', onDown)
      document.removeEventListener('keydown', onKey)
      window.removeEventListener('resize', onResize)
    }
  }, [open, close, setOpenState])

  useEffect(() => {
    if (!open || !pos) return
    const first = panelRef.current?.querySelector<HTMLElement>(FOCUSABLE)
    ;(first ?? panelRef.current)?.focus()
  }, [open, pos])

  const onPanelBlur = (e: ReactFocusEvent<HTMLDivElement>) => {
    const next = e.relatedTarget as Node | null
    if (!next) return
    if (panelRef.current?.contains(next) || triggerRef.current?.contains(next)) return
    setOpenState(false)
  }

  const triggerProps = trigger.props
  const clonedTrigger = cloneElement(trigger, {
    ref: (el: HTMLElement | null) => {
      triggerRef.current = el
      const r = (triggerProps as { ref?: unknown }).ref
      if (typeof r === 'function') r(el)
      else if (r && typeof r === 'object') (r as { current: HTMLElement | null }).current = el
    },
    'aria-haspopup': 'dialog',
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
              ref={panelRef}
              id={id}
              role="dialog"
              aria-label={label}
              tabIndex={-1}
              onBlur={onPanelBlur}
              style={{ top: pos?.top ?? -9999, left: pos?.left ?? -9999, width }}
              className={cn(
                'fixed z-[105] max-h-[calc(100vh-16px)] max-w-[calc(100vw-16px)] overflow-y-auto rounded-lg border border-border bg-surface-raised shadow-overlay outline-none scrollbar-thin',
                pos ? 'animate-scale-in' : 'opacity-0',
                className,
              )}
            >
              {typeof children === 'function' ? children(close) : children}
            </div>,
            document.body,
          )
        : null}
    </>
  )
}
