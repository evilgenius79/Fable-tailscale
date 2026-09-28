import { HardDrive } from 'lucide-react'
import type { Settings } from '../../api/types'
import { formatBytes, formatCompact, formatDateTime, formatInt, formatRelative } from '../../lib/format'
import { KeyValueList } from '../ui/KeyValue'
import { SkeletonText } from '../ui/Skeleton'
import { SettingsSection } from './SettingsSection'
import { StackedBar } from './StackedBar'
import { formatRetentionDays, formatRetentionHours, storeSegments, totalRows } from './settings'

export interface StorageSectionProps {
  settings: Settings | undefined
  loading: boolean
}

export function StorageSection({ settings, loading }: StorageSectionProps) {
  const store = settings?.store
  return (
    <SettingsSection id="storage" icon={HardDrive} title="Storage" description="A single SQLite database in the data directory. Raw samples are rolled up to 5-minute buckets and pruned hourly.">
      {loading && !settings ? (
        <SkeletonText lines={5} />
      ) : (
        <div className="grid gap-6 lg:grid-cols-[minmax(0,5fr)_minmax(0,7fr)]">
          <div>
            <p className="mb-1 text-[11px] font-semibold uppercase tracking-wider text-fg-muted">Retention</p>
            <KeyValueList
              items={[
                { key: 'Raw samples', value: formatRetentionHours(settings?.rawRetentionHours), hint: '--raw-retention' },
                { key: '5-minute rollups', value: formatRetentionDays(settings?.rollupRetentionDays), hint: '--rollup-retention' },
                { key: 'Events & alerts', value: formatRetentionDays(settings?.eventRetentionDays), hint: '--event-retention' },
              ]}
            />
            <p className="mt-3 text-[11px] leading-4 text-fg-faint">Nothing secret is stored: no API keys, tokens or webhook URLs. Back it up with sqlite3 .backup.</p>
          </div>
          <div>
            <p className="mb-1 text-[11px] font-semibold uppercase tracking-wider text-fg-muted">Database</p>
            <KeyValueList
              columns={2}
              items={[
                { key: 'Size on disk', value: formatBytes(store?.sizeBytes) },
                { key: 'Rows', value: formatCompact(totalRows(store)), hint: formatInt(totalRows(store)) },
                { key: 'Devices tracked', value: formatInt(store?.devices) },
                { key: 'Oldest sample', value: store?.oldestSample ? formatRelative(store.oldestSample) : '—', hint: store?.oldestSample ? formatDateTime(store.oldestSample) : undefined },
              ]}
            />
            <div className="mt-4">
              <StackedBar segments={storeSegments(store)} format={formatCompact} ariaLabel="Rows by table" />
            </div>
          </div>
        </div>
      )}
    </SettingsSection>
  )
}
