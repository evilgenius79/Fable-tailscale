import type { ReactNode } from 'react'
import { Button } from '../ui/Button'
import { SegmentedControl } from '../ui/SegmentedControl'
import { Select } from '../ui/Select'
import { osInfo } from '../../lib/os'
import { CheckChip } from './CheckChip'
import {
  DEFAULT_FILTERS,
  FLAGS,
  FLAG_META,
  PATH_OPTIONS,
  STATUS_OPTIONS,
  countActiveFilters,
  type DeviceFilters,
  type DeviceFlag,
  type Facet,
} from './deviceFilters'
import type { OSFamily } from '../../lib/os'

export interface FilterPanelProps {
  filters: DeviceFilters
  onChange: (patch: Partial<DeviceFilters>) => void
  onReplace: (next: DeviceFilters) => void
  os: Facet<OSFamily>[]
  users: Facet[]
  tags: Facet[]
  onClose: () => void
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <fieldset className="min-w-0">
      <legend className="mb-1.5 text-[11px] font-semibold uppercase tracking-wider text-fg-muted">{title}</legend>
      {children}
    </fieldset>
  )
}

function toggleIn<T>(list: readonly T[], v: T, on: boolean): T[] {
  return on ? (list.includes(v) ? [...list] : [...list, v]) : list.filter((x) => x !== v)
}

/** Body of the Filters popover: every dimension in one place, applied immediately. */
export function FilterPanel({ filters, onChange, onReplace, os, users, tags, onClose }: FilterPanelProps) {
  const active = countActiveFilters(filters)
  return (
    <div className="flex flex-col gap-4 p-3.5">
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <Section title="Status">
          <SegmentedControl aria-label="Status" size="sm" block options={STATUS_OPTIONS} value={filters.status} onValueChange={(status) => onChange({ status })} />
        </Section>
        <Section title="Path">
          <SegmentedControl aria-label="Path" size="sm" block options={PATH_OPTIONS} value={filters.path} onValueChange={(path) => onChange({ path })} />
        </Section>
      </div>
      <Section title="Operating system">
        {os.length ? (
          <div className="flex flex-wrap gap-1.5">
            {os.map((f) => (
              <CheckChip key={f.value} checked={filters.os.includes(f.value)} onCheckedChange={(on) => onChange({ os: toggleIn(filters.os, f.value, on) })} icon={osInfo(f.value).icon} count={f.count}>
                {f.label}
              </CheckChip>
            ))}
          </div>
        ) : (
          <p className="text-xs text-fg-muted">No devices yet.</p>
        )}
      </Section>
      <Section title="Flags">
        <div className="flex flex-wrap gap-1.5">
          {FLAGS.map((flag: DeviceFlag) => (
            <CheckChip key={flag} checked={filters.flags.includes(flag)} onCheckedChange={(on) => onChange({ flags: toggleIn(filters.flags, flag, on) })}>
              {FLAG_META[flag].label}
            </CheckChip>
          ))}
        </div>
      </Section>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <Section title="User">
          <Select
            aria-label="User"
            size="sm"
            className="w-full"
            value={filters.user}
            onValueChange={(user) => onChange({ user })}
            options={[{ value: '', label: 'Any user' }, ...users.map((u) => ({ value: u.value, label: `${u.label} (${u.count})` }))]}
          />
        </Section>
        <Section title="Tag">
          <Select
            aria-label="Tag"
            size="sm"
            className="w-full"
            value={filters.tag}
            onValueChange={(tag) => onChange({ tag })}
            options={[{ value: '', label: 'Any tag' }, ...tags.map((t) => ({ value: t.value, label: `${t.label} (${t.count})` }))]}
          />
        </Section>
      </div>
      <div className="flex items-center justify-between gap-2 border-t border-border pt-3">
        <Button variant="ghost" size="sm" disabled={!active} onClick={() => onReplace({ ...DEFAULT_FILTERS, q: filters.q })}>
          Clear filters
        </Button>
        <Button variant="primary" size="sm" onClick={onClose}>
          Done
        </Button>
      </div>
    </div>
  )
}
