import { Button } from '../ui/Button'
import { Toggle } from '../ui/Toggle'
import { COLUMNS, COLUMN_IDS, DEFAULT_VIEW_PREFS, toggleColumn, type DevicesViewPrefs } from './columns'

export interface ColumnsPanelProps {
  prefs: DevicesViewPrefs
  onChange: (next: DevicesViewPrefs) => void
}

/** Column chooser: "fit to width" auto mode or an explicit set (name is always shown). */
export function ColumnsPanel({ prefs, onChange }: ColumnsPanelProps) {
  const auto = prefs.columns === null
  const visible = new Set(auto ? COLUMN_IDS : ['name', ...prefs.columns!])
  return (
    <div className="flex flex-col gap-3 p-3.5">
      <Toggle
        checked={auto}
        onCheckedChange={(on) => onChange({ ...prefs, columns: on ? null : COLUMN_IDS.filter((c) => c !== 'name') })}
        label="Fit to width"
        description="Show more columns as the window grows"
        size="sm"
      />
      <ul className="-mx-1 grid grid-cols-2 gap-x-2" aria-label="Columns">
        {COLUMNS.map((c) => {
          const checked = visible.has(c.id)
          return (
            <li key={c.id}>
              <label className="flex cursor-pointer items-center gap-2 rounded-md px-1.5 py-1.5 text-[13px] text-fg hover:bg-surface-hover has-[:disabled]:cursor-default has-[:disabled]:text-fg-muted">
                <input
                  type="checkbox"
                  className="size-3.5 shrink-0 rounded accent-accent focus-ring"
                  checked={checked}
                  disabled={c.locked}
                  onChange={() => onChange(toggleColumn(prefs, c.id))}
                />
                <span className="truncate">{c.label}</span>
              </label>
            </li>
          )
        })}
      </ul>
      <div className="flex items-center justify-end border-t border-border pt-3">
        <Button variant="ghost" size="sm" disabled={auto && prefs.density === DEFAULT_VIEW_PREFS.density} onClick={() => onChange({ ...DEFAULT_VIEW_PREFS })}>
          Reset view
        </Button>
      </div>
    </div>
  )
}
