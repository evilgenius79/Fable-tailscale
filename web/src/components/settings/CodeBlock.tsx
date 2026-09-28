import { cn } from '../../lib/cn'
import { CopyButton } from '../ui/CopyButton'

export interface CodeBlockProps {
  code: string
  /** Accessible description for the copy button ("Copy install command"). */
  label?: string
  /** Shell prompt prefix rendered (not copied) before each line. */
  prompt?: string
  className?: string
}

/** Monospace, horizontally scrollable code with a copy button. Content is rendered as text only. */
export function CodeBlock({ code, label = 'Copy', prompt, className }: CodeBlockProps) {
  const lines = code.split('\n')
  return (
    <div className={cn('group relative rounded-md border border-border bg-surface-inset', className)}>
      <pre className="overflow-x-auto p-3 pr-12 font-mono text-[12px] leading-5 text-fg scrollbar-thin" tabIndex={0}>
        <code>
          {lines.map((line, i) => (
            <span key={i} className="block whitespace-pre">
              {prompt ? (
                <span className="select-none text-fg-faint" aria-hidden="true">
                  {prompt}
                </span>
              ) : null}
              {line || ' '}
            </span>
          ))}
        </code>
      </pre>
      <CopyButton value={code} label={label} size="xs" className="absolute right-1.5 top-1.5 bg-surface-inset/80 opacity-70 backdrop-blur group-hover:opacity-100 focus:opacity-100" />
    </div>
  )
}
