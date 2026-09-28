import type { CSSProperties } from 'react'
import { cn } from '../../lib/cn'

export interface SkeletonProps {
  className?: string
  width?: number | string
  height?: number | string
  rounded?: 'sm' | 'md' | 'lg' | 'full'
  style?: CSSProperties
}

const R = { sm: 'rounded-sm', md: 'rounded-md', lg: 'rounded-lg', full: 'rounded-full' }

/** Shimmering placeholder block. */
export function Skeleton({ className, width, height, rounded = 'sm', style }: SkeletonProps) {
  return <div aria-hidden="true" className={cn('skeleton', R[rounded], className)} style={{ width, height, ...style }} />
}

/** A few lines of text placeholder. */
export function SkeletonText({ lines = 3, className }: { lines?: number; className?: string }) {
  return (
    <div className={cn('space-y-2', className)} aria-hidden="true">
      {Array.from({ length: lines }, (_, i) => (
        <Skeleton key={i} height={12} width={i === lines - 1 ? '60%' : '100%'} />
      ))}
    </div>
  )
}

/** Card-shaped placeholder with a title bar and body lines. */
export function SkeletonCard({ className, lines = 3 }: { className?: string; lines?: number }) {
  return (
    <div className={cn('surface-card p-4 sm:p-5', className)} aria-hidden="true">
      <Skeleton height={14} width="40%" className="mb-4" />
      <SkeletonText lines={lines} />
    </div>
  )
}

/** Table-shaped placeholder. */
export function SkeletonTable({ rows = 6, cols = 5, className }: { rows?: number; cols?: number; className?: string }) {
  return (
    <div className={cn('surface-card overflow-hidden', className)} aria-hidden="true">
      <div className="flex gap-4 border-b border-border px-4 py-3">
        {Array.from({ length: cols }, (_, i) => (
          <Skeleton key={i} height={10} className="flex-1" />
        ))}
      </div>
      {Array.from({ length: rows }, (_, r) => (
        <div key={r} className="flex items-center gap-4 border-b border-border-subtle px-4 py-3 last:border-b-0">
          {Array.from({ length: cols }, (_, c) => (
            <Skeleton key={c} height={12} className="flex-1" width={c === 0 ? '30%' : undefined} />
          ))}
        </div>
      ))}
    </div>
  )
}

/** Full page placeholder used as the router Suspense fallback. */
export function PageSkeleton() {
  return (
    <div className="animate-fade-in space-y-6" aria-busy="true" aria-label="Loading page">
      <div className="space-y-2">
        <Skeleton height={24} width={220} />
        <Skeleton height={12} width={320} />
      </div>
      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        {Array.from({ length: 4 }, (_, i) => (
          <SkeletonCard key={i} lines={2} />
        ))}
      </div>
      <SkeletonTable />
    </div>
  )
}
