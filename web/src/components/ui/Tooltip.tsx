import {
  cloneElement,
  isValidElement,
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type FocusEvent,
  type PointerEvent,
  type ReactElement,
  type ReactNode,
} from 'react'
import { createPortal } from 'react-dom'
import { cn } from '../../lib/cn'

export type TooltipSide = 'top' | 'bottom' | 'left' | 'right'

export interface TooltipProps {
  content: ReactNode
  /** A single element that accepts pointer/focus handlers (button, span, a, …). */
  children: ReactElement<Record<string, unknown>>
  side?: TooltipSide
  /** ms hover delay before showing. */
  delay?: number
  disabled?: boolean
  className?: string
}

const GAP = 6

type Rect = { top: number; left: number; width: number; height: number }

/**
 * Lightweight tooltip. Attaches hover/focus handlers to its child (no wrapper
 * element), renders into a portal, and sets aria-describedby while visible.
 */
export function Tooltip({ content, children, side = 'top', delay = 250, disabled = false, className }: TooltipProps) {
  const id = useId()
  const [rect, setRect] = useState<Rect | null>(null)
  const [pos, setPos] = useState<{ top: number; left: number } | null>(null)
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const tipRef = useRef<HTMLDivElement | null>(null)

  const clear = () => {
    if (timer.current) clearTimeout(timer.current)
    timer.current = null
  }

  const show = useCallback(
    (el: Element, immediate: boolean) => {
      if (disabled) return
      clear()
      const doShow = () => {
        const r = el.getBoundingClientRect()
        setRect({ top: r.top, left: r.left, width: r.width, height: r.height })
      }
      if (immediate) doShow()
      else timer.current = setTimeout(doShow, delay)
    },
    [delay, disabled],
  )

  const hide = useCallback(() => {
    clear()
    setRect(null)
    setPos(null)
  }, [])

  useEffect(() => () => clear(), [])

  useEffect(() => {
    if (!rect) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') hide()
    }
    window.addEventListener('keydown', onKey)
    window.addEventListener('scroll', hide, true)
    return () => {
      window.removeEventListener('keydown', onKey)
      window.removeEventListener('scroll', hide, true)
    }
  }, [rect, hide])

  useLayoutEffect(() => {
    if (!rect || !tipRef.current) return
    const tip = tipRef.current.getBoundingClientRect()
    const vw = window.innerWidth
    const vh = window.innerHeight
    let top = 0
    let left = 0
    switch (side) {
      case 'bottom':
        top = rect.top + rect.height + GAP
        left = rect.left + rect.width / 2 - tip.width / 2
        break
      case 'left':
        top = rect.top + rect.height / 2 - tip.height / 2
        left = rect.left - GAP - tip.width
        break
      case 'right':
        top = rect.top + rect.height / 2 - tip.height / 2
        left = rect.left + rect.width + GAP
        break
      default:
        top = rect.top - GAP - tip.height
        left = rect.left + rect.width / 2 - tip.width / 2
    }
    if (top < 8) top = rect.top + rect.height + GAP
    if (top + tip.height > vh - 8) top = rect.top - GAP - tip.height
    left = Math.max(8, Math.min(left, vw - tip.width - 8))
    setPos({ top, left })
  }, [rect, side])

  if (!isValidElement(children)) return children
  const childProps = children.props

  const trigger = cloneElement(children, {
    'aria-describedby': rect ? id : (childProps['aria-describedby'] as string | undefined),
    onPointerEnter: (e: PointerEvent<Element>) => {
      ;(childProps.onPointerEnter as ((e: PointerEvent<Element>) => void) | undefined)?.(e)
      if (e.pointerType === 'touch') return
      show(e.currentTarget, false)
    },
    onPointerLeave: (e: PointerEvent<Element>) => {
      ;(childProps.onPointerLeave as ((e: PointerEvent<Element>) => void) | undefined)?.(e)
      hide()
    },
    onPointerDown: (e: PointerEvent<Element>) => {
      ;(childProps.onPointerDown as ((e: PointerEvent<Element>) => void) | undefined)?.(e)
      hide()
    },
    onFocus: (e: FocusEvent<Element>) => {
      ;(childProps.onFocus as ((e: FocusEvent<Element>) => void) | undefined)?.(e)
      if (e.currentTarget.matches(':focus-visible')) show(e.currentTarget, true)
    },
    onBlur: (e: FocusEvent<Element>) => {
      ;(childProps.onBlur as ((e: FocusEvent<Element>) => void) | undefined)?.(e)
      hide()
    },
  })

  return (
    <>
      {trigger}
      {rect && content
        ? createPortal(
            <div
              ref={tipRef}
              id={id}
              role="tooltip"
              style={{ top: pos?.top ?? -9999, left: pos?.left ?? -9999 }}
              className={cn(
                'pointer-events-none fixed z-[110] max-w-xs rounded-md border border-border bg-surface-raised px-2 py-1 text-xs font-medium leading-5 text-fg shadow-md',
                pos ? 'animate-fade-in' : 'opacity-0',
                className,
              )}
            >
              {content}
            </div>,
            document.body,
          )
        : null}
    </>
  )
}
