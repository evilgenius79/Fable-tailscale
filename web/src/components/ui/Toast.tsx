import { useEffect, useSyncExternalStore } from 'react'
import { AlertTriangle, CheckCircle2, Info, X, XCircle } from 'lucide-react'
import { cn } from '../../lib/cn'

export type ToastTone = 'info' | 'success' | 'warning' | 'error'

export interface ToastOptions {
  title: string
  description?: string
  tone?: ToastTone
  /** ms before auto-dismiss; 0 keeps it until dismissed. Default 5000 (8000 for warning/error). */
  duration?: number
  /** Stable id: a toast with the same id replaces the existing one instead of stacking. */
  id?: string
  action?: { label: string; onClick: () => void }
}

export interface ToastItem extends Required<Pick<ToastOptions, 'id' | 'title' | 'tone' | 'duration'>> {
  description?: string
  action?: ToastOptions['action']
  createdAt: number
}

// Module-level store so non-React code (sse.ts, mutations) can raise toasts.
let items: ToastItem[] = []
const listeners = new Set<() => void>()
const timers = new Map<string, ReturnType<typeof setTimeout>>()
let counter = 0
const MAX_VISIBLE = 5

function emit() {
  for (const l of listeners) l()
}

function schedule(t: ToastItem) {
  const existing = timers.get(t.id)
  if (existing) clearTimeout(existing)
  if (t.duration > 0) {
    timers.set(
      t.id,
      setTimeout(() => dismiss(t.id), t.duration),
    )
  }
}

function push(opts: ToastOptions): string {
  const tone = opts.tone ?? 'info'
  const id = opts.id ?? `t${++counter}`
  const duration = opts.duration ?? (tone === 'warning' || tone === 'error' ? 8000 : 5000)
  const item: ToastItem = { id, title: opts.title, description: opts.description, tone, duration, action: opts.action, createdAt: Date.now() }
  const idx = items.findIndex((t) => t.id === id)
  if (idx >= 0) items = items.map((t, i) => (i === idx ? item : t))
  else items = [...items, item].slice(-MAX_VISIBLE)
  schedule(item)
  emit()
  return id
}

function dismiss(id: string) {
  const t = timers.get(id)
  if (t) clearTimeout(t)
  timers.delete(id)
  if (!items.some((x) => x.id === id)) return
  items = items.filter((x) => x.id !== id)
  emit()
}

function clear() {
  for (const t of timers.values()) clearTimeout(t)
  timers.clear()
  items = []
  emit()
}

/**
 * Imperative toast API. `toast({ title })` or `toast.success(title, description?)`.
 * Usable anywhere, including outside React.
 */
export const toast = Object.assign(push, {
  info: (title: string, description?: string, opts?: Omit<ToastOptions, 'title' | 'description' | 'tone'>) => push({ ...opts, title, description, tone: 'info' }),
  success: (title: string, description?: string, opts?: Omit<ToastOptions, 'title' | 'description' | 'tone'>) => push({ ...opts, title, description, tone: 'success' }),
  warning: (title: string, description?: string, opts?: Omit<ToastOptions, 'title' | 'description' | 'tone'>) => push({ ...opts, title, description, tone: 'warning' }),
  error: (title: string, description?: string, opts?: Omit<ToastOptions, 'title' | 'description' | 'tone'>) => push({ ...opts, title, description, tone: 'error' }),
  dismiss,
  clear,
})

function subscribe(cb: () => void) {
  listeners.add(cb)
  return () => {
    listeners.delete(cb)
  }
}

/** Current toasts (subscribes to changes). */
export function useToasts(): ToastItem[] {
  return useSyncExternalStore(subscribe, () => items, () => items)
}

const ICONS: Record<ToastTone, typeof Info> = { info: Info, success: CheckCircle2, warning: AlertTriangle, error: XCircle }
const ICON_CLASS: Record<ToastTone, string> = {
  info: 'text-info',
  success: 'text-online',
  warning: 'text-warning',
  error: 'text-critical',
}
const TONE_LABEL: Record<ToastTone, string> = { info: 'Info', success: 'Success', warning: 'Warning', error: 'Error' }

function ToastCard({ item }: { item: ToastItem }) {
  const Icon = ICONS[item.tone]
  useEffect(() => {
    // Pause auto-dismiss while hovered is handled via pointer events below.
  }, [])
  return (
    <div
      role={item.tone === 'error' || item.tone === 'warning' ? 'alert' : 'status'}
      className={cn(
        'pointer-events-auto flex w-full items-start gap-3 rounded-lg border border-border bg-surface-raised p-3 pr-2 shadow-lg animate-slide-up',
      )}
      onPointerEnter={() => {
        const t = timers.get(item.id)
        if (t) clearTimeout(t)
      }}
      onPointerLeave={() => schedule(item)}
    >
      <Icon className={cn('mt-0.5 size-4 shrink-0', ICON_CLASS[item.tone])} aria-hidden="true" />
      <div className="min-w-0 flex-1">
        <p className="text-[13px] font-medium leading-5 text-fg">
          <span className="sr-only">{TONE_LABEL[item.tone]}: </span>
          {item.title}
        </p>
        {item.description ? <p className="mt-0.5 text-xs leading-5 text-fg-secondary break-words">{item.description}</p> : null}
        {item.action ? (
          <button
            type="button"
            onClick={() => {
              item.action?.onClick()
              dismiss(item.id)
            }}
            className="mt-1.5 text-xs font-medium text-accent-text hover:underline focus-ring rounded"
          >
            {item.action.label}
          </button>
        ) : null}
      </div>
      <button
        type="button"
        aria-label="Dismiss notification"
        onClick={() => dismiss(item.id)}
        className="rounded-md p-1 text-fg-muted hover:bg-surface-hover hover:text-fg focus-ring"
      >
        <X className="size-3.5" aria-hidden="true" />
      </button>
    </div>
  )
}

/** Mount once (App does). Renders the toast viewport bottom-right. */
export function Toaster() {
  const list = useToasts()
  return (
    <div
      aria-live="polite"
      aria-relevant="additions"
      className="pointer-events-none fixed inset-x-4 bottom-4 z-[100] flex flex-col items-end gap-2 sm:inset-x-auto sm:right-4 sm:w-[360px]"
    >
      {list.map((t) => (
        <ToastCard key={t.id} item={t} />
      ))}
    </div>
  )
}
