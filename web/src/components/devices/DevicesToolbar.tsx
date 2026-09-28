import { useEffect, useRef, type ReactNode } from 'react'
import { ArrowDown, ArrowUp, ArrowUpDown, ChevronDown, Columns3, Funnel, RefreshCw, Rows3, Rows4 } from 'lucide-react'
import type { OSFamily } from '../../lib/os'
import { Button } from '../ui/Button'
import { SearchInput } from '../ui/Input'
import { Kbd } from '../ui/Kbd'
import { DropdownMenu, type MenuItem } from '../ui/Menu'
import { SegmentedControl } from '../ui/SegmentedControl'
import type { SortState } from '../ui/Table'
import { ColumnsPanel } from './ColumnsPanel'
import { FilterPanel } from './FilterPanel'
import { Popover } from './Popover'
import { SORT_MENU, type Density, type DevicesViewPrefs } from './columns'
import { PATH_OPTIONS, STATUS_OPTIONS, countActiveFilters, type DeviceFilters, type Facet } from './deviceFilters'

export interface DevicesToolbarProps {
  filters: DeviceFilters
  onFiltersChange: (patch: Partial<DeviceFilters>) => void
  onFiltersReplace: (next: DeviceFilters) => void
  sort: SortState
  onSortChange: (next: SortState) => void
  prefs: DevicesViewPrefs
  onPrefsChange: (next: DevicesViewPrefs) => void
  facets: { os: Facet<OSFamily>[]; users: Facet[]; tags: Facet[] }
  /** Table layout is active (column + density controls only make sense there). */
  tableLayout: boolean
  isAdmin: boolean
  refreshing?: boolean
  onRefresh?: () => void
  /** Result count line, rendered at the start of the second row. */
  summary?: ReactNode
  /** Active-filter chips, rendered after the summary. */
  chips?: ReactNode
}

const DENSITY_OPTIONS: ReadonlyArray<{ value: Density; icon: typeof Rows3; ariaLabel: string }> = [
  { value: 'comfortable', icon: Rows3, ariaLabel: 'Comfortable rows' },
  { value: 'compact', icon: Rows4, ariaLabel: 'Compact rows' },
]

/**
 * Two rows: search + filters + sort (+ admin refresh) above, result count +
 * active chips + view controls (columns, density) below.
 */
export function DevicesToolbar({
  filters,
  onFiltersChange,
  onFiltersReplace,
  sort,
  onSortChange,
  prefs,
  onPrefsChange,
  facets,
  tableLayout,
  isAdmin,
  refreshing,
  onRefresh,
  summary,
  chips,
}: DevicesToolbarProps) {
  const searchRef = useRef<HTMLInputElement>(null)
  const activeCount = countActiveFilters(filters)

  // "/" focuses the search box (unless typing somewhere else).
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== '/' || e.metaKey || e.ctrlKey || e.altKey) return
      const t = e.target as HTMLElement | null
      if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.tagName === 'SELECT' || t.isContentEditable)) return
      e.preventDefault()
      searchRef.current?.focus()
      searchRef.current?.select()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  const sortLabel = SORT_MENU.find((s) => s.key === sort.key)?.label ?? sort.key
  const sortItems: MenuItem[] = [
    ...SORT_MENU.map((s) => ({
      id: s.key,
      label: s.label,
      checked: s.key === sort.key,
      hint: s.key === sort.key ? (sort.dir === 'asc' ? '↑' : '↓') : undefined,
      onSelect: () => onSortChange({ key: s.key, dir: s.key === sort.key ? sort.dir : 'asc' }),
    })),
    { id: 'dir:asc', label: 'Ascending', icon: ArrowUp, checked: sort.dir === 'asc', separatorBefore: true, onSelect: () => onSortChange({ ...sort, dir: 'asc' }) },
    { id: 'dir:desc', label: 'Descending', icon: ArrowDown, checked: sort.dir === 'desc', onSelect: () => onSortChange({ ...sort, dir: 'desc' }) },
  ]

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <SearchInput
          ref={searchRef}
          size="sm"
          value={filters.q}
          onValueChange={(q) => onFiltersChange({ q })}
          placeholder="Search name, IP, user, tag…"
          aria-label="Search devices"
          shortcut={<Kbd className="mr-0.5 hidden sm:inline-flex">/</Kbd>}
          className="w-full sm:w-64 2xl:w-80"
        />
        <div className="hidden items-center gap-2 xl:flex">
          <SegmentedControl aria-label="Status" size="sm" options={STATUS_OPTIONS} value={filters.status} onValueChange={(status) => onFiltersChange({ status })} />
          <SegmentedControl aria-label="Path" size="sm" options={PATH_OPTIONS} value={filters.path} onValueChange={(path) => onFiltersChange({ path })} />
        </div>
        <Popover
          label="Filters"
          width={400}
          trigger={
            <Button size="sm" leadingIcon={Funnel} trailingIcon={ChevronDown} aria-label={activeCount ? `Filters, ${activeCount} active` : 'Filters'}>
              Filters
              {activeCount ? <span className="num rounded-md bg-accent px-1.5 py-0.5 text-[11px] font-semibold leading-none text-accent-fg">{activeCount}</span> : null}
            </Button>
          }
        >
          {(close) => <FilterPanel filters={filters} onChange={onFiltersChange} onReplace={onFiltersReplace} os={facets.os} users={facets.users} tags={facets.tags} onClose={close} />}
        </Popover>
        <DropdownMenu
          label="Sort devices"
          width={220}
          items={sortItems}
          trigger={
            <Button size="sm" leadingIcon={ArrowUpDown} trailingIcon={ChevronDown} aria-label={`Sort by ${sortLabel}, ${sort.dir === 'asc' ? 'ascending' : 'descending'}`}>
              <span className="hidden sm:inline">Sort: </span>
              <span className="max-w-24 truncate">{sortLabel}</span>
              {sort.dir === 'asc' ? <ArrowUp className="size-3 text-fg-muted" aria-hidden="true" /> : <ArrowDown className="size-3 text-fg-muted" aria-hidden="true" />}
            </Button>
          }
        />
        {isAdmin && onRefresh ? (
          <Button size="sm" leadingIcon={RefreshCw} loading={refreshing} onClick={onRefresh} className="ml-auto">
            Refresh
          </Button>
        ) : null}
      </div>
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
        <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-2 text-xs text-fg-muted">
          {summary}
          {chips}
        </div>
        {tableLayout ? (
          <div className="ml-auto flex items-center gap-2">
            <Popover
              label="Columns"
              width={320}
              align="end"
              trigger={
                <Button size="sm" variant="ghost" leadingIcon={Columns3} trailingIcon={ChevronDown} aria-label="Choose columns">
                  Columns
                </Button>
              }
            >
              <ColumnsPanel prefs={prefs} onChange={onPrefsChange} />
            </Popover>
            <SegmentedControl
              aria-label="Row density"
              size="sm"
              value={prefs.density}
              onValueChange={(density) => onPrefsChange({ ...prefs, density })}
              options={DENSITY_OPTIONS.map((o) => ({ value: o.value, label: '', icon: o.icon, ariaLabel: o.ariaLabel }))}
            />
          </div>
        ) : null}
      </div>
    </div>
  )
}
