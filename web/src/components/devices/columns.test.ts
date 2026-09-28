import { describe, expect, it } from 'vitest'
import { COLUMNS, COLUMN_IDS, DEFAULT_VIEW_PREFS, SORT_MENU, columnById, isColumnId, sanitizeViewPrefs, toggleColumn, visibleColumns } from './columns'

describe('columns catalogue', () => {
  it('has unique ids and a locked name column', () => {
    expect(new Set(COLUMN_IDS).size).toBe(COLUMNS.length)
    expect(columnById('name').locked).toBe(true)
    expect(isColumnId('cpu')).toBe(true)
    expect(isColumnId('bogus')).toBe(false)
  })
  it('offers status first in the sort menu, then every sortable column', () => {
    expect(SORT_MENU[0]).toEqual({ key: 'status', label: 'Status' })
    expect(SORT_MENU.map((s) => s.key)).toContain('throughput')
  })
})

describe('view prefs', () => {
  it('auto mode shows every column', () => {
    expect(visibleColumns(DEFAULT_VIEW_PREFS).map((c) => c.id)).toEqual(COLUMN_IDS)
  })
  it('toggling leaves auto mode and keeps catalogue order', () => {
    const p1 = toggleColumn(DEFAULT_VIEW_PREFS, 'os')
    expect(p1.columns).not.toBeNull()
    expect(p1.columns).not.toContain('os')
    expect(p1.columns).not.toContain('name')
    expect(visibleColumns(p1).map((c) => c.id)).toEqual(COLUMN_IDS.filter((c) => c !== 'os'))
    const p2 = toggleColumn(p1, 'os')
    expect(visibleColumns(p2).map((c) => c.id)).toEqual(COLUMN_IDS)
    expect(toggleColumn(p1, 'name')).toBe(p1)
  })
  it('always includes the name column even if the explicit set omits it', () => {
    expect(visibleColumns({ density: 'compact', columns: ['ip'] }).map((c) => c.id)).toEqual(['name', 'ip'])
  })
  it('sanitises persisted junk', () => {
    expect(sanitizeViewPrefs(null)).toEqual(DEFAULT_VIEW_PREFS)
    expect(sanitizeViewPrefs('nope')).toEqual(DEFAULT_VIEW_PREFS)
    expect(sanitizeViewPrefs({ density: 'huge', columns: 'all' })).toEqual({ density: 'comfortable', columns: null })
    expect(sanitizeViewPrefs({ density: 'compact', columns: ['name', 'ip', 'bogus', 42] })).toEqual({ density: 'compact', columns: ['ip'] })
  })
})
