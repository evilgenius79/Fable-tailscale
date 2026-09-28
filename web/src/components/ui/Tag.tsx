import { X } from 'lucide-react'
import { cn } from '../../lib/cn'
import { tagLabel } from '../../lib/format'

export interface TagProps {
  /** Tailscale ACL tag, e.g. "tag:server". The "tag:" prefix is shown de-emphasised. */
  tag: string
  size?: 'sm' | 'md'
  onRemove?: () => void
  className?: string
}

/** Monospace chip for ACL tags. */
export function Tag({ tag, size = 'md', onRemove, className }: TagProps) {
  const label = tagLabel(tag)
  const prefixed = tag.startsWith('tag:')
  return (
    <span
      className={cn(
        'inline-flex max-w-full items-center gap-0.5 rounded-md border border-border bg-surface-inset font-mono text-fg-secondary',
        size === 'sm' ? 'h-5 px-1.5 text-[11px]' : 'h-6 px-2 text-xs',
        className,
      )}
    >
      {prefixed ? <span className="text-fg-faint">tag:</span> : null}
      <span className="truncate text-fg">{label}</span>
      {onRemove ? (
        <button
          type="button"
          onClick={onRemove}
          aria-label={`Remove ${tag}`}
          className="-mr-1 ml-0.5 rounded p-0.5 text-fg-muted hover:bg-surface-hover hover:text-fg focus-ring"
        >
          <X className="size-3" aria-hidden="true" />
        </button>
      ) : null}
    </span>
  )
}

export function TagList({ tags, size = 'sm', max = 3, className }: { tags: string[]; size?: 'sm' | 'md'; max?: number; className?: string }) {
  if (!tags.length) return <span className="text-fg-faint">—</span>
  const shown = tags.slice(0, max)
  const rest = tags.length - shown.length
  return (
    <span className={cn('inline-flex flex-wrap items-center gap-1', className)}>
      {shown.map((t) => (
        <Tag key={t} tag={t} size={size} />
      ))}
      {rest > 0 ? (
        <span className="text-xs text-fg-muted" title={tags.slice(max).join(', ')}>
          +{rest}
        </span>
      ) : null}
    </span>
  )
}
