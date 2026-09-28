import { cn } from '../../lib/cn'

/** Tailwatch mark: a radar sweep in the accent colour. */
export function LogoMark({ className, size = 24 }: { className?: string; size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" aria-hidden="true" className={cn('shrink-0', className)}>
      <rect x="1" y="1" width="22" height="22" rx="6" className="fill-accent" />
      <path d="M12 12 L12 5.5" stroke="white" strokeWidth="1.8" strokeLinecap="round" opacity="0.95" />
      <path d="M12 5.5 A6.5 6.5 0 0 1 18.5 12" stroke="white" strokeWidth="1.8" strokeLinecap="round" opacity="0.95" />
      <path d="M12 8.5 A3.5 3.5 0 0 1 15.5 12" stroke="white" strokeWidth="1.8" strokeLinecap="round" opacity="0.6" />
      <circle cx="12" cy="12" r="1.6" fill="white" />
      <path d="M5.5 12 A6.5 6.5 0 0 0 12 18.5" stroke="white" strokeWidth="1.8" strokeLinecap="round" opacity="0.35" />
    </svg>
  )
}

export function Wordmark({ className }: { className?: string }) {
  return <span className={cn('text-[15px] font-semibold tracking-tight text-fg', className)}>Tailwatch</span>
}
