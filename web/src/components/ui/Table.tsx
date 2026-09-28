import { createContext, useContext, useState, type ComponentProps, type CSSProperties, type KeyboardEvent, type ReactNode } from 'react'
import { useNavigate } from 'react-router-dom'
import { ArrowDown, ArrowUp, ChevronsUpDown } from 'lucide-react'
import { cn } from '../../lib/cn'

export type SortDir = 'asc' | 'desc'
export interface SortState {
  key: string
  dir: SortDir
}
export type SortValue = string | number | boolean | Date | null | undefined

/** Sort state with a toggle: first click asc, second desc. */
export function useSort(initial: SortState | null = null) {
  const [sort, setSort] = useState<SortState | null>(initial)
  const toggle = (key: string) =>
    setSort((prev) => (prev?.key === key ? { key, dir: prev.dir === 'asc' ? 'desc' : 'asc' } : { key, dir: 'asc' }))
  return { sort, setSort, toggle }
}

function cmp(a: SortValue, b: SortValue): number {
  const an = a === null || a === undefined || (typeof a === 'number' && Number.isNaN(a))
  const bn = b === null || b === undefined || (typeof b === 'number' && Number.isNaN(b))
  if (an && bn) return 0
  if (an) return 1
  if (bn) return -1
  if (a instanceof Date && b instanceof Date) return a.getTime() - b.getTime()
  if (typeof a === 'number' && typeof b === 'number') return a - b
  if (typeof a === 'boolean' && typeof b === 'boolean') return Number(a) - Number(b)
  return String(a).localeCompare(String(b), undefined, { numeric: true, sensitivity: 'base' })
}

/** Stable sort by an accessor map. Nullish values always sort last. */
export function sortRows<T>(rows: ReadonlyArray<T>, sort: SortState | null, accessors: Record<string, (row: T) => SortValue>): T[] {
  if (!sort) return [...rows]
  const acc = accessors[sort.key]
  if (!acc) return [...rows]
  const dir = sort.dir === 'asc' ? 1 : -1
  return rows
    .map((row, i) => ({ row, i, v: acc(row) }))
    .sort((x, y) => {
      const c = cmp(x.v, y.v)
      // keep nullish last regardless of direction
      if (c !== 0 && (x.v === null || x.v === undefined || y.v === null || y.v === undefined)) return c
      return (c || x.i - y.i) * (c ? dir : 1)
    })
    .map((x) => x.row)
}

type HideBelow = 'sm' | 'md' | 'lg' | 'xl'
const HIDE: Record<HideBelow, string> = { sm: 'hidden sm:table-cell', md: 'hidden md:table-cell', lg: 'hidden lg:table-cell', xl: 'hidden xl:table-cell' }
const ALIGN = { left: 'text-left', right: 'text-right', center: 'text-center' }

const TableCtx = createContext<{ dense: boolean; sticky: boolean }>({ dense: false, sticky: true })

export interface TableProps extends ComponentProps<'table'> {
  /** Sticky header while the container scrolls (default true). */
  stickyHeader?: boolean
  dense?: boolean
  /** Container max height; the header sticks while the body scrolls. */
  maxHeight?: number | string
  /** Minimum table width before horizontal scrolling kicks in. */
  minWidth?: number | string
  /** Draw the card-like container (default true). Set false inside a Card. */
  bordered?: boolean
  containerClassName?: string
}

/** Scrollable table container. Compose with THead/TBody/TR/TH/TD. */
export function Table({ stickyHeader = true, dense = false, maxHeight, minWidth, bordered = true, containerClassName, className, children, ...rest }: TableProps) {
  return (
    <TableCtx.Provider value={{ dense, sticky: stickyHeader }}>
      <div
        className={cn('relative w-full overflow-auto scrollbar-thin', bordered && 'rounded-lg border border-border bg-surface shadow-xs', containerClassName)}
        style={{ maxHeight }}
      >
        <table className={cn('w-full border-collapse text-sm', className)} style={{ minWidth }} {...rest}>
          {children}
        </table>
      </div>
    </TableCtx.Provider>
  )
}

export function THead({ className, ...rest }: ComponentProps<'thead'>) {
  const { sticky } = useContext(TableCtx)
  return <thead className={cn(sticky && 'sticky top-0 z-10', className)} {...rest} />
}

export function TBody({ className, ...rest }: ComponentProps<'tbody'>) {
  return <tbody className={cn('[&>tr:last-child>td]:border-b-0', className)} {...rest} />
}

export interface TRProps extends ComponentProps<'tr'> {
  /** Hover + focus styles and keyboard activation (Enter/Space call onClick). */
  interactive?: boolean
  selected?: boolean
  /** Navigate on click (also makes the row interactive). */
  to?: string
}

export function TR({ interactive, selected, to, onClick, onKeyDown, className, tabIndex, ...rest }: TRProps) {
  const navigate = useNavigate()
  const clickable = interactive || !!to || !!onClick
  const handleClick = (e: React.MouseEvent<HTMLTableRowElement>) => {
    onClick?.(e)
    if (to && !e.defaultPrevented) {
      const target = e.target as HTMLElement
      if (target.closest('a,button,input,select,textarea,[role="menu"]')) return
      navigate(to)
    }
  }
  const handleKey = (e: KeyboardEvent<HTMLTableRowElement>) => {
    onKeyDown?.(e)
    if (!clickable || e.defaultPrevented) return
    if ((e.key === 'Enter' || e.key === ' ') && e.target === e.currentTarget) {
      e.preventDefault()
      if (to) navigate(to)
      else onClick?.(e as unknown as React.MouseEvent<HTMLTableRowElement>)
    }
  }
  return (
    <tr
      tabIndex={clickable ? (tabIndex ?? 0) : tabIndex}
      onClick={clickable ? handleClick : onClick}
      onKeyDown={handleKey}
      aria-selected={selected}
      className={cn(
        'group/row transition-colors',
        clickable && 'cursor-pointer hover:bg-surface-hover focus-ring-inset',
        selected && 'bg-accent-soft hover:bg-accent-soft-hover',
        className,
      )}
      {...rest}
    />
  )
}

export interface THProps extends ComponentProps<'th'> {
  sortKey?: string
  sort?: SortState | null
  onSort?: (key: string) => void
  align?: 'left' | 'right' | 'center'
  width?: number | string
  hideBelow?: HideBelow
  /** Tooltip/description for the column. */
  hint?: string
}

export function TH({ sortKey, sort, onSort, align = 'left', width, hideBelow, hint, className, children, style, ...rest }: THProps) {
  const { dense } = useContext(TableCtx)
  const sortable = !!sortKey && !!onSort
  const active = sortable && sort?.key === sortKey
  const ariaSort = active ? (sort?.dir === 'asc' ? 'ascending' : 'descending') : sortable ? 'none' : undefined
  const Icon = active ? (sort?.dir === 'asc' ? ArrowUp : ArrowDown) : ChevronsUpDown
  return (
    <th
      scope="col"
      aria-sort={ariaSort}
      title={hint}
      style={{ width, ...style } as CSSProperties}
      className={cn(
        'bg-surface hairline-b whitespace-nowrap text-[11px] font-semibold uppercase tracking-wider text-fg-muted',
        dense ? 'h-8 px-3' : 'h-10 px-4',
        ALIGN[align],
        hideBelow && HIDE[hideBelow],
        className,
      )}
      {...rest}
    >
      {sortable ? (
        <button
          type="button"
          onClick={() => onSort(sortKey)}
          className={cn('-mx-1 inline-flex items-center gap-1 rounded px-1 py-0.5 hover:text-fg focus-ring', active && 'text-fg')}
        >
          <span>{children}</span>
          <Icon className={cn('size-3.5', active ? 'text-fg' : 'text-fg-faint')} aria-hidden="true" />
        </button>
      ) : (
        children
      )}
    </th>
  )
}

export interface TDProps extends ComponentProps<'td'> {
  align?: 'left' | 'right' | 'center'
  /** Tabular figures, right aligned. */
  numeric?: boolean
  /** Ellipsis overflow; requires a width on the column or table-fixed. */
  truncate?: boolean
  hideBelow?: HideBelow
  muted?: boolean
  mono?: boolean
}

export function TD({ align, numeric, truncate, hideBelow, muted, mono, className, children, ...rest }: TDProps) {
  const { dense } = useContext(TableCtx)
  const a = align ?? (numeric ? 'right' : 'left')
  return (
    <td
      className={cn(
        'border-b border-border-subtle align-middle text-fg',
        dense ? 'h-9 px-3 py-1.5 text-[13px]' : 'h-12 px-4 py-2',
        ALIGN[a],
        numeric && 'num',
        truncate && 'max-w-0 truncate',
        muted && 'text-fg-muted',
        mono && 'font-mono text-xs',
        hideBelow && HIDE[hideBelow],
        className,
      )}
      {...rest}
    >
      {children}
    </td>
  )
}

/** Full-width row used for empty/loading/error states inside a table body. */
export function TableMessage({ colSpan, children, className }: { colSpan: number; children: ReactNode; className?: string }) {
  return (
    <tr>
      <td colSpan={colSpan} className={cn('px-4 py-10 text-center text-sm text-fg-muted', className)}>
        {children}
      </td>
    </tr>
  )
}
