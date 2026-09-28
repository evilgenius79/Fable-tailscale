import { X } from 'lucide-react'
import { osInfo, type OSFamily } from '../../lib/os'
import { Button } from '../ui/Button'
import { DEFAULT_FILTERS, FLAG_META, PATH_OPTIONS, STATUS_OPTIONS, countActiveFilters, type DeviceFilters, type DeviceFlag } from './deviceFilters'

export interface ActiveFilterChipsProps {
  filters: DeviceFilters
  onChange: (patch: Partial<DeviceFilters>) => void
  onReplace: (next: DeviceFilters) => void
  /** Display label for a user login. */
  userLabel?: (login: string) => string
}

interface Chip {
  key: string
  label: string
  remove: () => void
}

function Chiplet({ chip }: { chip: Chip }) {
  return (
    <span className="inline-flex h-6 max-w-full items-center gap-1 rounded-md border border-border bg-surface pl-2 pr-0.5 text-xs text-fg">
      <span className="truncate">{chip.label}</span>
      <button type="button" onClick={chip.remove} aria-label={`Remove filter ${chip.label}`} className="rounded p-0.5 text-fg-muted hover:bg-surface-hover hover:text-fg focus-ring">
        <X className="size-3" aria-hidden="true" />
      </button>
    </span>
  )
}

/** Row of removable chips mirroring the active filters (search excluded — it has its own clear button). */
export function ActiveFilterChips({ filters, onChange, onReplace, userLabel }: ActiveFilterChipsProps) {
  const chips: Chip[] = []
  if (filters.status !== 'all') chips.push({ key: 'status', label: STATUS_OPTIONS.find((o) => o.value === filters.status)?.label ?? filters.status, remove: () => onChange({ status: 'all' }) })
  if (filters.path !== 'all') chips.push({ key: 'path', label: PATH_OPTIONS.find((o) => o.value === filters.path)?.label ?? filters.path, remove: () => onChange({ path: 'all' }) })
  for (const fam of filters.os as OSFamily[]) chips.push({ key: `os:${fam}`, label: fam === 'other' ? 'Other OS' : osInfo(fam).label, remove: () => onChange({ os: filters.os.filter((x) => x !== fam) }) })
  if (filters.user) chips.push({ key: 'user', label: `User: ${userLabel ? userLabel(filters.user) : filters.user}`, remove: () => onChange({ user: '' }) })
  if (filters.tag) chips.push({ key: 'tag', label: filters.tag, remove: () => onChange({ tag: '' }) })
  for (const flag of filters.flags as DeviceFlag[]) chips.push({ key: `flag:${flag}`, label: FLAG_META[flag].label, remove: () => onChange({ flags: filters.flags.filter((x) => x !== flag) }) })
  if (!chips.length) return null
  return (
    <div className="flex flex-wrap items-center gap-1.5" aria-label={`${countActiveFilters(filters)} active filters`}>
      {chips.map((c) => (
        <Chiplet key={c.key} chip={c} />
      ))}
      <Button variant="link" size="xs" className="ml-1 text-xs" onClick={() => onReplace({ ...DEFAULT_FILTERS, q: filters.q })}>
        Clear all
      </Button>
    </div>
  )
}
