import { useEffect, useRef, useState } from 'react'
import { Check, Copy } from 'lucide-react'
import { cn } from '../../lib/cn'
import { buttonClass, type ButtonSize } from './Button'
import { Tooltip } from './Tooltip'

export interface CopyButtonProps {
  value: string
  /** Accessible label; defaults to "Copy". */
  label?: string
  size?: ButtonSize
  /** Show the label text next to the icon. */
  withText?: boolean
  className?: string
}

async function copyText(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text)
      return true
    }
  } catch {
    /* fall through */
  }
  try {
    const ta = document.createElement('textarea')
    ta.value = text
    ta.setAttribute('readonly', '')
    ta.style.position = 'fixed'
    ta.style.opacity = '0'
    document.body.appendChild(ta)
    ta.select()
    const ok = document.execCommand('copy')
    document.body.removeChild(ta)
    return ok
  } catch {
    return false
  }
}

/** Copies `value` to the clipboard and confirms with a check mark for 1.5s. */
export function CopyButton({ value, label = 'Copy', size = 'sm', withText, className }: CopyButtonProps) {
  const [state, setState] = useState<'idle' | 'copied' | 'failed'>('idle')
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null)
  useEffect(() => () => {
    if (timer.current) clearTimeout(timer.current)
  }, [])
  const onClick = async () => {
    const ok = await copyText(value)
    setState(ok ? 'copied' : 'failed')
    if (timer.current) clearTimeout(timer.current)
    timer.current = setTimeout(() => setState('idle'), 1500)
  }
  const text = state === 'copied' ? 'Copied' : state === 'failed' ? 'Copy failed' : label
  const Icon = state === 'copied' ? Check : Copy
  return (
    <Tooltip content={text}>
      <button
        type="button"
        onClick={() => void onClick()}
        aria-label={withText ? undefined : text}
        aria-live="polite"
        className={cn(buttonClass({ variant: 'ghost', size, iconOnly: !withText }), state === 'copied' && 'text-online', className)}
      >
        <Icon aria-hidden="true" />
        {withText ? <span>{text}</span> : null}
      </button>
    </Tooltip>
  )
}
