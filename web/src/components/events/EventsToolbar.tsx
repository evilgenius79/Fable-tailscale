import { useEffect, useRef, type ReactNode } from 'react'
import { X } from 'lucide-react'
import type { EventType, Severity } from '../../api/types'
import { eventTypeLabel } from '../../lib/status'
import { Button } from '../ui/Button'
import { SearchInput } from '../ui/Input'
import { Kbd } from '../ui/Kbd'
import { SegmentedControl } from '../ui/SegmentedControl'
import { Select } from '../ui/Select'
import { TypeFilter } from './TypeFilter'
import { DEFAULT_EVENT_FILTERS, SINCE_PRESETS, countActiveEventFilters, type EventFilters, type SincePreset } from './events'

export interface EventsToolbarProps {
  filters: EventFilters
  onChange: (patch: Partial<EventFilters>) => void
  onReset: () => void
  devices: { value: string; label: string }[]
  typeCounts?: Partial<Record<EventType, number>>
  /** Result-count line for the second row. */
  summary?: ReactNode
}

const SEVERITY_OPTIONS: { value: 'all' | Severity; label: string }[] = [
  { value: 'all', label: 'Any severity' },
  { value: 'critical', label: 'Critical' },
  { value: 'warning', label: 'Warning' },
  { value: 'info', label: 'Info' },
]

function Chip({ label, onRemove }: { label: string; onRemove: () => void }) {
  return (
    <span className="inline-flex h-6 max-w-full items-center gap-1 rounded-md border border-border bg-surface pl-2 pr-0.5 text-xs text-fg">
      <span className="truncate">{label}</span>
      <button type="button" onClick={onRemove} aria-label={`Remove filter ${label}`} className="rounded p-0.5 text-fg-muted hover:bg-surface-hover hover:text-fg focus-ring">
        <X className="size-3" aria-hidden="true" />
      </button>
    </span>
  )
}

/** Search + type/severity/device/since filters above; summary + active chips below. */
export function EventsToolbar({ filters, onChange, onReset, devices, typeCounts, summary }: EventsToolbarProps) {
  const searchRef = useRef<HTMLInputElement>(null)
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

  const active = countActiveEventFilters(filters)
  const deviceLabel = devices.find((d) => d.value === filters.device)?.label ?? filters.device
  const chips: { key: string; label: string; remove: () => void }[] = []
  if (filters.types.length) chips.push({ key: 'types', label: filters.types.length === 1 ? eventTypeLabel(filters.types[0]!) : `${filters.types.length} types`, remove: () => onChange({ types: [] }) })
  if (filters.severity !== 'all') chips.push({ key: 'sev', label: SEVERITY_OPTIONS.find((o) => o.value === filters.severity)?.label ?? filters.severity, remove: () => onChange({ severity: 'all' }) })
  if (filters.device) chips.push({ key: 'device', label: `Device: ${deviceLabel}`, remove: () => onChange({ device: '' }) })
  if (filters.since !== DEFAULT_EVENT_FILTERS.since) chips.push({ key: 'since', label: filters.since === 'all' ? 'All time' : `Last ${filters.since}`, remove: () => onChange({ since: DEFAULT_EVENT_FILTERS.since }) })

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <SearchInput
          ref={searchRef}
          size="sm"
          value={filters.q}
          onValueChange={(q) => onChange({ q })}
          placeholder="Search title, message, device…"
          aria-label="Search events"
          shortcut={<Kbd className="mr-0.5 hidden sm:inline-flex">/</Kbd>}
          className="w-full sm:w-64 2xl:w-80"
        />
        <TypeFilter value={filters.types} onChange={(types) => onChange({ types })} counts={typeCounts} />
        <Select size="sm" aria-label="Severity" value={filters.severity} onValueChange={(severity) => onChange({ severity })} options={SEVERITY_OPTIONS} className="w-[140px]" />
        <Select
          size="sm"
          aria-label="Device"
          value={filters.device}
          onValueChange={(device) => onChange({ device })}
          options={[{ value: '', label: 'Any device' }, ...devices]}
          className="w-[160px]"
        />
        <SegmentedControl<SincePreset>
          aria-label="Time window"
          size="sm"
          value={filters.since}
          onValueChange={(since) => onChange({ since })}
          options={SINCE_PRESETS.map((p) => ({ value: p.value, label: p.label, ariaLabel: p.value === 'all' ? 'All time' : `Last ${p.label}` }))}
        />
      </div>
      <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-2 text-xs text-fg-muted">
        {summary}
        {chips.length ? (
          <div className="flex flex-wrap items-center gap-1.5" aria-label={`${active} active filters`}>
            {chips.map((c) => (
              <Chip key={c.key} label={c.label} onRemove={c.remove} />
            ))}
            <Button variant="link" size="xs" className="ml-1 text-xs" onClick={onReset}>
              Clear all
            </Button>
          </div>
        ) : null}
      </div>
    </div>
  )
}
