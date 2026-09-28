import { useEffect, useRef, type ReactNode } from 'react'
import { X } from 'lucide-react'
import type { Severity } from '../../api/types'
import { Button } from '../ui/Button'
import { SearchInput } from '../ui/Input'
import { Kbd } from '../ui/Kbd'
import { SegmentedControl } from '../ui/SegmentedControl'
import { Select } from '../ui/Select'
import { DEFAULT_ALERT_FILTERS, hasAlertFilters, type AlertFilters, type SeverityFilter } from './alerts'

export interface AlertsToolbarProps {
  filters: AlertFilters
  onChange: (patch: Partial<AlertFilters>) => void
  onReset: () => void
  devices: { value: string; label: string }[]
  /** Counts per severity for the current tab (shown in the segmented control). */
  counts?: Partial<Record<Severity, number>>
  summary?: ReactNode
}

const SEVERITIES: { value: SeverityFilter; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'critical', label: 'Critical' },
  { value: 'warning', label: 'Warning' },
  { value: 'info', label: 'Info' },
]

/** Search, severity and device filters for the Open/Resolved tabs; active chips below. */
export function AlertsToolbar({ filters, onChange, onReset, devices, counts, summary }: AlertsToolbarProps) {
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

  const deviceLabel = devices.find((d) => d.value === filters.device)?.label ?? filters.device
  const chips: { key: string; label: string; remove: () => void }[] = []
  if (filters.severity !== 'all') chips.push({ key: 'sev', label: SEVERITIES.find((s) => s.value === filters.severity)?.label ?? filters.severity, remove: () => onChange({ severity: 'all' }) })
  if (filters.device) chips.push({ key: 'device', label: `Device: ${deviceLabel}`, remove: () => onChange({ device: '' }) })

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <SearchInput
          ref={searchRef}
          size="sm"
          value={filters.q}
          onValueChange={(q) => onChange({ q })}
          placeholder="Search alerts…"
          aria-label="Search alerts"
          shortcut={<Kbd className="mr-0.5 hidden sm:inline-flex">/</Kbd>}
          className="w-full sm:w-64 2xl:w-80"
        />
        <SegmentedControl<SeverityFilter>
          aria-label="Severity"
          size="sm"
          value={filters.severity}
          onValueChange={(severity) => onChange({ severity })}
          options={SEVERITIES.map((s) => ({
            value: s.value,
            label: (
              <span className="inline-flex items-center gap-1.5">
                {s.label}
                {counts && s.value !== 'all' && counts[s.value] ? <span className="num text-[11px] text-fg-muted">{counts[s.value]}</span> : null}
              </span>
            ),
            ariaLabel: s.value === 'all' ? 'All severities' : s.label,
          }))}
        />
        <Select
          size="sm"
          aria-label="Device"
          value={filters.device}
          onValueChange={(device) => onChange({ device })}
          options={[{ value: '', label: 'Any device' }, ...devices]}
          className="w-[170px]"
        />
      </div>
      <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-2 text-xs text-fg-muted">
        {summary}
        {chips.length ? (
          <div className="flex flex-wrap items-center gap-1.5" aria-label={`${chips.length} active filters`}>
            {chips.map((c) => (
              <span key={c.key} className="inline-flex h-6 max-w-full items-center gap-1 rounded-md border border-border bg-surface pl-2 pr-0.5 text-xs text-fg">
                <span className="truncate">{c.label}</span>
                <button type="button" onClick={c.remove} aria-label={`Remove filter ${c.label}`} className="rounded p-0.5 text-fg-muted hover:bg-surface-hover hover:text-fg focus-ring">
                  <X className="size-3" aria-hidden="true" />
                </button>
              </span>
            ))}
            {hasAlertFilters(filters) ? (
              <Button variant="link" size="xs" className="ml-1 text-xs" onClick={() => onReset()}>
                Clear all
              </Button>
            ) : null}
          </div>
        ) : null}
      </div>
    </div>
  )
}

export { DEFAULT_ALERT_FILTERS }
