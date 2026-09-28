// Column catalogue for the Devices table. `autoClass` carries the responsive
// visibility used in "Auto" column mode as container queries against the
// table's scroll container (so the sidebar state is accounted for; Tailwind
// needs the literal strings). Explicit column selections show every chosen
// column at every width and let the table scroll horizontally instead.
//
// Each breakpoint is the summed intrinsic width of every column visible at
// that point plus a small margin (measured in both densities with
// scripts/check-table-fit.cjs), so the auto set never overflows the container.

export type ColumnId =
  | 'name'
  | 'os'
  | 'ip'
  | 'user'
  | 'path'
  | 'latency'
  | 'throughput'
  | 'cpu'
  | 'mem'
  | 'disk'
  | 'uptime'
  | 'version'
  | 'keyExpiry'
  | 'lastSeen'

export interface ColumnDef {
  id: ColumnId
  label: string
  /** Shorter header label for dense tables. */
  short?: string
  sortKey?: string
  align?: 'left' | 'right'
  hint?: string
  /** Responsive visibility classes applied in Auto mode. */
  autoClass?: string
  /** Cannot be hidden. */
  locked?: boolean
}

export const COLUMNS: readonly ColumnDef[] = [
  { id: 'name', label: 'Device', sortKey: 'name', locked: true, hint: 'Status, name, hostname and tags' },
  { id: 'os', label: 'OS', sortKey: 'os', autoClass: 'hidden @min-[1020px]:table-cell', hint: 'Operating system and model' },
  { id: 'ip', label: 'Tailscale IP', short: 'IP', sortKey: 'ip' },
  { id: 'user', label: 'User', sortKey: 'user', autoClass: 'hidden @min-[1590px]:table-cell' },
  { id: 'path', label: 'Path', sortKey: 'path', hint: 'Direct peer-to-peer or via a DERP relay' },
  { id: 'latency', label: 'Latency', sortKey: 'latency', align: 'right', autoClass: 'hidden @min-[860px]:table-cell', hint: 'Last disco ping round trip' },
  { id: 'throughput', label: 'Throughput', sortKey: 'throughput', align: 'right', hint: 'Live Tailscale receive / transmit rate' },
  { id: 'cpu', label: 'CPU', sortKey: 'cpu', autoClass: 'hidden @min-[1120px]:table-cell', hint: 'From tailwatch-agent' },
  { id: 'mem', label: 'Memory', short: 'Mem', sortKey: 'mem', autoClass: 'hidden @min-[1330px]:table-cell', hint: 'From tailwatch-agent' },
  { id: 'disk', label: 'Disk', sortKey: 'disk', autoClass: 'hidden @min-[1330px]:table-cell', hint: 'Fullest filesystem, from tailwatch-agent' },
  { id: 'uptime', label: 'Uptime 24h', short: 'Up 24h', sortKey: 'uptime', align: 'right', autoClass: 'hidden @min-[1830px]:table-cell', hint: 'Share of the last 24 hours the device was online' },
  { id: 'version', label: 'Version', sortKey: 'version', autoClass: 'hidden @min-[1460px]:table-cell', hint: 'Tailscale client version' },
  { id: 'keyExpiry', label: 'Key expiry', sortKey: 'keyExpiry', autoClass: 'hidden @min-[1720px]:table-cell', hint: 'Node key expiry' },
  { id: 'lastSeen', label: 'Last seen', sortKey: 'lastSeen', align: 'right', autoClass: 'hidden @min-[860px]:table-cell' },
]

export const COLUMN_IDS: readonly ColumnId[] = COLUMNS.map((c) => c.id)

export function isColumnId(v: unknown): v is ColumnId {
  return typeof v === 'string' && (COLUMN_IDS as readonly string[]).includes(v)
}

export function columnById(id: ColumnId): ColumnDef {
  return COLUMNS.find((c) => c.id === id) ?? COLUMNS[0]!
}

/** Sort keys offered in the Sort menu (in column order, status first). */
export const SORT_MENU: ReadonlyArray<{ key: string; label: string }> = [
  { key: 'status', label: 'Status' },
  ...COLUMNS.filter((c): c is ColumnDef & { sortKey: string } => !!c.sortKey).map((c) => ({ key: c.sortKey, label: c.label })),
]

export type Density = 'comfortable' | 'compact'

export interface DevicesViewPrefs {
  density: Density
  /** null = Auto (responsive); otherwise the explicit visible set (name is always included). */
  columns: ColumnId[] | null
}

export const DEFAULT_VIEW_PREFS: DevicesViewPrefs = { density: 'comfortable', columns: null }

/** Resolve the visible column list (Auto keeps every column and relies on autoClass). */
export function visibleColumns(prefs: DevicesViewPrefs): ColumnDef[] {
  if (!prefs.columns) return [...COLUMNS]
  const set = new Set<ColumnId>(['name', ...prefs.columns])
  return COLUMNS.filter((c) => set.has(c.id))
}

/** Toggle a column; leaving Auto mode materialises the full set first. */
export function toggleColumn(prefs: DevicesViewPrefs, id: ColumnId): DevicesViewPrefs {
  if (columnById(id).locked) return prefs
  const current = prefs.columns ?? [...COLUMN_IDS]
  const next = current.includes(id) ? current.filter((c) => c !== id) : COLUMN_IDS.filter((c) => c === id || current.includes(c))
  return { ...prefs, columns: next.filter((c) => c !== 'name') }
}

/** Parse untrusted persisted prefs (localStorage) into a valid shape. */
export function sanitizeViewPrefs(raw: unknown): DevicesViewPrefs {
  if (!raw || typeof raw !== 'object') return DEFAULT_VIEW_PREFS
  const o = raw as { density?: unknown; columns?: unknown }
  const density: Density = o.density === 'compact' ? 'compact' : 'comfortable'
  const columns = Array.isArray(o.columns) ? o.columns.filter(isColumnId).filter((c) => c !== 'name') : null
  return { density, columns }
}
