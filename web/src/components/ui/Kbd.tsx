import type { ComponentProps } from 'react'
import { cn } from '../../lib/cn'

/** True on macOS/iOS where the command key is ⌘. */
export function isMacPlatform(): boolean {
  if (typeof navigator === 'undefined') return false
  const p = (navigator as { userAgentData?: { platform?: string } }).userAgentData?.platform ?? navigator.platform ?? ''
  return /mac|iphone|ipad|ipod/i.test(p)
}

export function Kbd({ className, ...rest }: ComponentProps<'kbd'>) {
  return (
    <kbd
      className={cn(
        'inline-flex h-5 min-w-5 items-center justify-center rounded border border-border bg-surface-inset px-1 font-sans text-[11px] font-medium text-fg-muted shadow-[inset_0_-1px_0_var(--border)]',
        className,
      )}
      {...rest}
    />
  )
}

const LABELS: Record<string, string> = { Mod: isMacPlatform() ? '⌘' : 'Ctrl', Shift: '⇧', Alt: isMacPlatform() ? '⌥' : 'Alt', Enter: '↵', Esc: 'Esc', Up: '↑', Down: '↓' }

/** Renders a key combination, e.g. <Shortcut keys={['Mod', 'K']} /> → ⌘ K. */
export function Shortcut({ keys, className }: { keys: string[]; className?: string }) {
  return (
    <span className={cn('inline-flex items-center gap-0.5', className)} aria-label={keys.join('+')}>
      {keys.map((k, i) => (
        <Kbd key={i}>{LABELS[k] ?? k}</Kbd>
      ))}
    </span>
  )
}
