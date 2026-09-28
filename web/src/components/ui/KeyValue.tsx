import type { ReactNode } from 'react'
import { cn } from '../../lib/cn'
import { CopyButton } from './CopyButton'

export interface KeyValueItem {
  key: ReactNode
  value: ReactNode
  /** Monospace value (ids, addresses, versions). */
  mono?: boolean
  /** Text to copy; shows a copy button on hover. */
  copy?: string
  /** Span both columns in 2-column layouts. */
  span?: boolean
  hint?: string
}

export interface KeyValueListProps {
  items: KeyValueItem[]
  columns?: 1 | 2 | 3
  dense?: boolean
  /** Rows separated by hairlines (default) or plain stacked pairs. */
  divided?: boolean
  className?: string
}

/** Definition list of label/value pairs. */
export function KeyValueList({ items, columns = 1, dense, divided = true, className }: KeyValueListProps) {
  return (
    <dl
      className={cn(
        'grid gap-x-6',
        columns === 2 && 'sm:grid-cols-2',
        columns === 3 && 'sm:grid-cols-2 lg:grid-cols-3',
        !divided && (dense ? 'gap-y-2' : 'gap-y-3'),
        className,
      )}
    >
      {items.map((it, i) => (
        <KeyValue key={i} label={it.key} mono={it.mono} copy={it.copy} hint={it.hint} dense={dense} divided={divided} className={cn(it.span && 'sm:col-span-full')}>
          {it.value}
        </KeyValue>
      ))}
    </dl>
  )
}

export interface KeyValueProps {
  label: ReactNode
  children: ReactNode
  mono?: boolean
  copy?: string
  hint?: string
  dense?: boolean
  divided?: boolean
  className?: string
}

export function KeyValue({ label, children, mono, copy, hint, dense, divided = true, className }: KeyValueProps) {
  return (
    <div className={cn('group flex min-w-0 items-baseline justify-between gap-4', divided && 'border-b border-border-subtle last:border-b-0', dense ? 'py-1.5' : 'py-2', className)}>
      <dt className="shrink-0 text-[13px] text-fg-muted" title={hint}>
        {label}
      </dt>
      <dd className={cn('flex min-w-0 items-center gap-1 text-right text-[13px] text-fg', mono && 'font-mono text-xs')}>
        <span className="min-w-0 truncate">{children ?? <span className="text-fg-faint">—</span>}</span>
        {copy ? <CopyButton value={copy} size="xs" className="opacity-0 group-hover:opacity-100 focus:opacity-100" /> : null}
      </dd>
    </div>
  )
}
