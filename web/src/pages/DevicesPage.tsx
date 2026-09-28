import { useMemo } from 'react'
import { ArrowDown, ArrowUp } from 'lucide-react'
import { useDevices, useIsAdmin, useRefresh } from '../api/hooks'
import { ActiveFilterChips } from '../components/devices/ActiveFilterChips'
import { DeviceCards } from '../components/devices/DeviceCards'
import { DevicesTable } from '../components/devices/DevicesTable'
import { DevicesToolbar } from '../components/devices/DevicesToolbar'
import { visibleColumns } from '../components/devices/columns'
import { DEFAULT_FILTERS, applyFilters, hasAnyFilter, osFacets, sortDevices, tagFacets, totalRates, userFacets } from '../components/devices/deviceFilters'
import { useDevicesView, useMediaQuery, useRateHistory, useViewPrefs } from '../components/devices/useDevicesView'
import { ErrorState } from '../components/ui/ErrorState'
import { PageHeader } from '../components/ui/PageHeader'
import { toast } from '../components/ui/Toast'
import { formatBitrate, formatInt, plural } from '../lib/format'

export default function DevicesPage() {
  const q = useDevices()
  const isAdmin = useIsAdmin()
  const refresh = useRefresh()
  const view = useDevicesView()
  const [prefs, setPrefs] = useViewPrefs()
  const tableLayout = useMediaQuery('(min-width: 768px)')
  const history = useRateHistory(q.data)

  const all = useMemo(() => q.data ?? [], [q.data])
  // One timestamp per data change keeps relative times consistent across the table.
  const now = useMemo(() => Date.now(), [all])
  const filtered = useMemo(() => sortDevices(applyFilters(all, view.filters, now), view.sort), [all, view.filters, view.sort, now])
  const facets = useMemo(() => ({ os: osFacets(all), users: userFacets(all), tags: tagFacets(all) }), [all])
  const userLabel = (login: string) => facets.users.find((u) => u.value === login)?.label ?? login
  const rates = totalRates(filtered)
  const filtering = hasAnyFilter(view.filters)
  const columns = useMemo(() => visibleColumns(prefs), [prefs])
  const clearFilters = () => view.replaceFilters({ ...DEFAULT_FILTERS })

  const onRefresh = () =>
    refresh.mutate(undefined, {
      onSuccess: () => toast.info('Refresh requested', 'The hub is polling the tailnet now.', { duration: 2500 }),
    })

  return (
    <>
      <PageHeader
        title="Devices"
        description="Every node on the tailnet with status, path, versions and live metrics."
        meta={
          q.data ? (
            <>
              <span className="num">{plural(all.length, 'device')}</span>
              <span aria-hidden="true">·</span>
              <span className="num">{formatInt(all.filter((d) => d.online).length)} online</span>
            </>
          ) : null
        }
      />

      <div className="space-y-3">
        <DevicesToolbar
          filters={view.filters}
          onFiltersChange={view.setFilters}
          onFiltersReplace={view.replaceFilters}
          sort={view.sort}
          onSortChange={view.setSort}
          prefs={prefs}
          onPrefsChange={setPrefs}
          facets={facets}
          tableLayout={tableLayout}
          isAdmin={isAdmin}
          refreshing={refresh.isPending}
          onRefresh={onRefresh}
          summary={
            <p className="num" aria-live="polite">
              {q.isPending ? (
                'Loading devices…'
              ) : filtering ? (
                <>
                  <span className="font-medium text-fg">{formatInt(filtered.length)}</span> of {plural(all.length, 'device')}
                </>
              ) : (
                <span className="font-medium text-fg">{plural(all.length, 'device')}</span>
              )}
              {q.data && rates.online ? (
                <>
                  <span aria-hidden="true"> · </span>
                  <span>{formatInt(rates.online)} online</span>
                  <span aria-hidden="true"> · </span>
                  <span className="inline-flex items-center gap-0.5 whitespace-nowrap">
                    <ArrowDown className="size-3 text-fg-faint" aria-hidden="true" />
                    <span className="sr-only">receive </span>
                    {formatBitrate(rates.rx)}
                  </span>
                  <span className="ml-1.5 inline-flex items-center gap-0.5 whitespace-nowrap">
                    <ArrowUp className="size-3 text-fg-faint" aria-hidden="true" />
                    <span className="sr-only">transmit </span>
                    {formatBitrate(rates.tx)}
                  </span>
                </>
              ) : null}
            </p>
          }
          chips={<ActiveFilterChips filters={view.filters} onChange={view.setFilters} onReplace={view.replaceFilters} userLabel={userLabel} />}
        />

        {q.error && !q.data ? (
          <ErrorState error={q.error} onRetry={() => void q.refetch()} retrying={q.isFetching} />
        ) : tableLayout ? (
          <DevicesTable
            devices={filtered}
            total={all.length}
            columns={columns}
            autoColumns={prefs.columns === null}
            density={prefs.density}
            sort={view.sort}
            onSort={view.toggleSort}
            history={history}
            loading={q.isPending}
            now={now}
            onClearFilters={clearFilters}
          />
        ) : (
          <DeviceCards devices={filtered} total={all.length} history={history} loading={q.isPending} now={now} onClearFilters={clearFilters} />
        )}
        {q.error && q.data ? <ErrorState compact error={q.error} title="Live data may be stale" onRetry={() => void q.refetch()} retrying={q.isFetching} /> : null}
      </div>
    </>
  )
}
