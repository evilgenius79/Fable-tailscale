import { cn } from '../../lib/cn'

export interface SpinnerProps {
  size?: 'xs' | 'sm' | 'md' | 'lg'
  className?: string
  label?: string
}

const SIZES = { xs: 'size-3 border-[1.5px]', sm: 'size-4 border-2', md: 'size-5 border-2', lg: 'size-8 border-[3px]' }

/** Circular indeterminate spinner (currentColor). */
export function Spinner({ size = 'sm', className, label = 'Loading' }: SpinnerProps) {
  return (
    <span
      role="status"
      aria-label={label}
      className={cn('inline-block shrink-0 animate-spin-slow rounded-full border-current border-r-transparent', SIZES[size], className)}
    />
  )
}
