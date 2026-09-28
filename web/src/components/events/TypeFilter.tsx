import { useEffect, useRef } from 'react'
import { ChevronDown, ListFilter } from 'lucide-react'
import type { EventType } from '../../api/types'
import { cn } from '../../lib/cn'
import { eventTypeLabel } from '../../lib/status'
import { Button } from '../ui/Button'
import { Popover } from './Popover'
import { ALL_EVENT_TYPES, EVENT_GROUPS, eventIcon } from './events'

export interface TypeFilterProps {
  value: EventType[]
  onChange: (types: EventType[]) => void
  /** Facet counts for the loaded events (optional). */
  counts?: Partial<Record<EventType, number>>
}

function GroupCheckbox({ checked, indeterminate, onChange, label, count }: { checked: boolean; indeterminate: boolean; onChange: (v: boolean) => void; label: string; count?: number }) {
  const ref = useRef<HTMLInputElement>(null)
  useEffect(() => {
    if (ref.current) ref.current.indeterminate = indeterminate
  }, [indeterminate])
  return (
    <label className="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-[13px] font-semibold text-fg hover:bg-surface-hover">
      <input ref={ref} type="checkbox" checked={checked} onChange={(e) => onChange(e.target.checked)} className="size-3.5 accent-[var(--accent)]" />
      <span className="flex-1">{label}</span>
      {count !== undefined ? <span className="num text-[11px] font-medium text-fg-muted">{count}</span> : null}
    </label>
  )
}

/** Grouped multi-select of event types (device / agent / alert / admin / hub) in a non-modal popover. */
export function TypeFilter({ value, onChange, counts }: TypeFilterProps) {
  const selected = new Set<EventType>(value)
  const n = value.length
  const label = n === 0 ? 'All types' : n === 1 ? eventTypeLabel(value[0]!) : `${n} types`
  const toggle = (t: EventType, on: boolean) => {
    const next = new Set(selected)
    if (on) next.add(t)
    else next.delete(t)
    onChange(ALL_EVENT_TYPES.filter((x) => next.has(x)))
  }
  const toggleGroup = (types: readonly EventType[], on: boolean) => {
    const next = new Set(selected)
    for (const t of types) if (on) next.add(t)
    else next.delete(t)
    onChange(ALL_EVENT_TYPES.filter((x) => next.has(x)))
  }
  return (
    <Popover
      label="Filter by event type"
      width={300}
      trigger={
        <Button size="sm" leadingIcon={ListFilter} trailingIcon={ChevronDown} aria-label={`Event types: ${label}`} className={cn(n > 0 && 'border-accent/40 text-accent-text')}>
          <span className="max-w-32 truncate">{label}</span>
        </Button>
      }
    >
      {(close) => (
        <div className="p-2">
          <div className="max-h-[min(60vh,420px)] space-y-1 overflow-y-auto scrollbar-thin">
            {EVENT_GROUPS.map((g) => {
              const on = g.types.filter((t) => selected.has(t)).length
              const groupCount = counts ? g.types.reduce((a, t) => a + (counts[t] ?? 0), 0) : undefined
              return (
                <div key={g.id}>
                  <GroupCheckbox label={g.label} checked={on === g.types.length} indeterminate={on > 0 && on < g.types.length} onChange={(v) => toggleGroup(g.types, v)} count={groupCount} />
                  <ul className="ml-3 border-l border-border-subtle pl-1">
                    {g.types.map((t) => {
                      const Icon = eventIcon(t)
                      return (
                        <li key={t}>
                          <label className="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1 text-[13px] text-fg-secondary hover:bg-surface-hover hover:text-fg">
                            <input type="checkbox" checked={selected.has(t)} onChange={(e) => toggle(t, e.target.checked)} className="size-3.5 accent-[var(--accent)]" />
                            <Icon className="size-3.5 text-fg-muted" aria-hidden="true" />
                            <span className="flex-1 truncate">{eventTypeLabel(t)}</span>
                            {counts ? <span className="num text-[11px] text-fg-muted">{counts[t] ?? 0}</span> : null}
                          </label>
                        </li>
                      )
                    })}
                  </ul>
                </div>
              )
            })}
          </div>
          <div className="mt-2 flex items-center justify-between border-t border-border pt-2">
            <Button size="xs" variant="ghost" onClick={() => onChange([])} disabled={n === 0}>
              Clear
            </Button>
            <Button size="xs" variant="primary" onClick={close}>
              Done
            </Button>
          </div>
        </div>
      )}
    </Popover>
  )
}
