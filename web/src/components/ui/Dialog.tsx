import { useEffect, useId, useRef, type ReactNode, type RefObject } from 'react'
import { createPortal } from 'react-dom'
import { X } from 'lucide-react'
import { cn } from '../../lib/cn'
import { FOCUSABLE, useFocusTrap } from '../../lib/useFocusTrap'
import { Button, type ButtonVariant } from './Button'

export type DialogSize = 'sm' | 'md' | 'lg' | 'xl' | 'full'

export interface DialogProps {
  open: boolean
  onClose: () => void
  title: ReactNode
  description?: ReactNode
  children?: ReactNode
  footer?: ReactNode
  size?: DialogSize
  /** Element to focus when opened; defaults to the first focusable element. */
  initialFocus?: RefObject<HTMLElement | null>
  closeOnBackdrop?: boolean
  hideClose?: boolean
  className?: string
  /** Remove body padding (tables, lists). */
  flush?: boolean
}

const SIZES: Record<DialogSize, string> = {
  sm: 'sm:max-w-sm',
  md: 'sm:max-w-md',
  lg: 'sm:max-w-lg',
  xl: 'sm:max-w-2xl',
  full: 'sm:max-w-[min(96vw,1200px)]',
}

/**
 * Accessible modal: portal, focus trap, Escape/backdrop close, scroll lock,
 * focus restored to the opener. Renders nothing when closed.
 */
export function Dialog({ open, onClose, title, description, children, footer, size = 'md', initialFocus, closeOnBackdrop = true, hideClose, className, flush }: DialogProps) {
  const id = useId()
  const panelRef = useRef<HTMLDivElement>(null)
  const openerRef = useRef<Element | null>(null)
  // Callers routinely pass an inline `onClose`; keep it in a ref so a new
  // identity on every parent render (e.g. each SSE tick) does not re-run the
  // open effect, which would steal focus / blur inputs and toggle the scroll lock.
  const onCloseRef = useRef(onClose)
  onCloseRef.current = onClose

  useFocusTrap(panelRef, open)

  useEffect(() => {
    if (!open) return
    openerRef.current = document.activeElement
    const prevOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    const focusTarget = initialFocus?.current ?? panelRef.current?.querySelector<HTMLElement>(FOCUSABLE) ?? panelRef.current
    const raf = requestAnimationFrame(() => focusTarget?.focus())
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return
      e.stopPropagation()
      onCloseRef.current()
    }
    document.addEventListener('keydown', onKey, true)
    return () => {
      cancelAnimationFrame(raf)
      document.removeEventListener('keydown', onKey, true)
      document.body.style.overflow = prevOverflow
      const opener = openerRef.current
      if (opener instanceof HTMLElement && document.contains(opener)) opener.focus()
    }
  }, [open, initialFocus])

  if (!open) return null

  return createPortal(
    <div className="fixed inset-0 z-[90] flex items-end justify-center sm:items-center sm:p-4" role="presentation">
      <div className="absolute inset-0 bg-overlay animate-fade-in backdrop-blur-[2px]" onClick={closeOnBackdrop ? onClose : undefined} aria-hidden="true" />
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={`${id}-title`}
        aria-describedby={description ? `${id}-desc` : undefined}
        tabIndex={-1}
        className={cn(
          'relative flex max-h-[92dvh] w-full flex-col rounded-t-xl border border-border bg-surface-raised shadow-overlay outline-none animate-slide-up sm:rounded-xl sm:animate-scale-in',
          SIZES[size],
          className,
        )}
      >
        <div className="flex items-start justify-between gap-4 px-5 pt-5 pb-3">
          <div className="min-w-0">
            <h2 id={`${id}-title`} className="text-base font-semibold leading-6 text-fg">
              {title}
            </h2>
            {description ? (
              <p id={`${id}-desc`} className="mt-1 text-sm leading-5 text-fg-muted">
                {description}
              </p>
            ) : null}
          </div>
          {!hideClose ? (
            <button type="button" onClick={onClose} aria-label="Close dialog" className="-mr-1.5 -mt-1.5 rounded-md p-1.5 text-fg-muted hover:bg-surface-hover hover:text-fg focus-ring">
              <X className="size-4" aria-hidden="true" />
            </button>
          ) : null}
        </div>
        <div className={cn('min-h-0 flex-1 overflow-y-auto scrollbar-thin', flush ? '' : 'px-5 pb-5', !footer && !flush && 'pb-5')}>{children}</div>
        {footer ? <div className="flex flex-wrap items-center justify-end gap-2 border-t border-border px-5 py-3">{footer}</div> : null}
      </div>
    </div>,
    document.body,
  )
}

export interface ConfirmDialogProps {
  open: boolean
  onClose: () => void
  onConfirm: () => void | Promise<void>
  title: ReactNode
  description?: ReactNode
  confirmLabel?: string
  cancelLabel?: string
  tone?: 'primary' | 'danger'
  loading?: boolean
  children?: ReactNode
}

/** Two-button confirmation. Focus lands on Cancel for destructive tones. */
export function ConfirmDialog({ open, onClose, onConfirm, title, description, confirmLabel = 'Confirm', cancelLabel = 'Cancel', tone = 'primary', loading, children }: ConfirmDialogProps) {
  const cancelRef = useRef<HTMLButtonElement>(null)
  const variant: ButtonVariant = tone === 'danger' ? 'danger' : 'primary'
  return (
    <Dialog
      open={open}
      onClose={onClose}
      title={title}
      description={description}
      size="sm"
      initialFocus={tone === 'danger' ? cancelRef : undefined}
      footer={
        <>
          <Button ref={cancelRef} variant="ghost" onClick={onClose} disabled={loading}>
            {cancelLabel}
          </Button>
          <Button variant={variant} onClick={() => void onConfirm()} loading={loading}>
            {confirmLabel}
          </Button>
        </>
      }
    >
      {children}
    </Dialog>
  )
}
