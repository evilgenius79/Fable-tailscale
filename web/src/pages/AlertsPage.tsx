import { useCallback, useEffect, useMemo, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { Bell, BellOff, BookOpen, Send, SlidersHorizontal } from 'lucide-react'
import { useAckAlert, useAlerts, useDevices, useIsAdmin, useRules, useSettings } from '../api/hooks'
import type { Alert, AlertRule, Severity } from '../api/types'
import { AlertList } from '../components/alerts/AlertList'
import { AlertsToolbar } from '../components/alerts/AlertsToolbar'
import { RuleCards } from '../components/alerts/RuleCards'
import { RuleEditorDialog } from '../components/alerts/RuleEditorDialog'
import { RuleTypeGuide } from '../components/alerts/RuleTypeGuide'
import { RulesTable } from '../components/alerts/RulesTable'
import { TestNotificationDialog } from '../components/alerts/TestNotificationDialog'
import { DEFAULT_ALERT_FILTERS, alertDeviceOptions, alertFiltersFromSearch, filterAlerts, groupOpenAlerts, groupResolvedAlerts, hasAlertFilters, type AlertFilters } from '../components/alerts/alerts'
import { useRuleSave } from '../components/alerts/useRuleSave'
import { Button, buttonClass } from '../components/ui/Button'
import { PageHeader } from '../components/ui/PageHeader'
import { TabPanel, Tabs } from '../components/ui/Tabs'
import { toast } from '../components/ui/Toast'
import { formatInt, plural } from '../lib/format'

type Tab = 'open' | 'resolved' | 'rules'

function isTab(v: string | null): v is Tab {
  return v === 'open' || v === 'resolved' || v === 'rules'
}

export default function AlertsPage() {
  const [sp, setSp] = useSearchParams()
  const tab: Tab = isTab(sp.get('tab')) ? (sp.get('tab') as Tab) : 'open'
  const setTab = (next: Tab) =>
    setSp(
      (prev) => {
        const p = new URLSearchParams(prev)
        if (next === 'open') p.delete('tab')
        else p.set('tab', next)
        p.delete('rule')
        return p
      },
      { replace: true },
    )

  const isAdmin = useIsAdmin()
  const openQ = useAlerts({ state: 'open', limit: 200 })
  const resolvedQ = useAlerts({ state: 'resolved', limit: 200 })
  const rulesQ = useRules()
  const devicesQ = useDevices()
  const settingsQ = useSettings()
  const ack = useAckAlert()
  const { saveRule, pendingId } = useRuleSave()

  const [filters, setFilters] = useState<AlertFilters>(() => alertFiltersFromSearch(sp))
  const patchFilters = useCallback((p: Partial<AlertFilters>) => setFilters((f) => ({ ...f, ...p })), [])
  const resetFilters = useCallback(() => setFilters((f) => ({ ...DEFAULT_ALERT_FILTERS, q: f.q })), [])

  // Deep links (`?severity=` from the overview KPIs, `?device=` from a device's
  // "Alert history"). Keyed on the parameter values, not the params object, so
  // tab / rule-editor navigation (which copies the query string) does not
  // re-apply a filter the user has since cleared.
  const severityParam = sp.get('severity')
  const deviceParam = sp.get('device')
  useEffect(() => {
    setFilters((f) => alertFiltersFromSearch(new URLSearchParams({ ...(severityParam ? { severity: severityParam } : {}), ...(deviceParam ? { device: deviceParam } : {}) }), f))
  }, [severityParam, deviceParam])

  const source = tab === 'resolved' ? resolvedQ : openQ
  const list = useMemo(() => source.data ?? [], [source.data])
  const now = useMemo(() => Date.now(), [list])
  const filtered = useMemo(() => filterAlerts(list, filters), [list, filters])
  const groups = useMemo(() => (tab === 'resolved' ? groupResolvedAlerts(filtered, new Date(now)) : groupOpenAlerts(filtered)), [tab, filtered, now])
  const deviceOptions = useMemo(() => {
    const opts = alertDeviceOptions(list)
    // A device from the deep link that has no alert in this list still needs an option (and a name if we know it).
    if (filters.device && !opts.some((o) => o.value === filters.device)) {
      opts.push({ value: filters.device, label: devicesQ.data?.find((d) => d.id === filters.device)?.name ?? filters.device })
    }
    return opts
  }, [list, filters.device, devicesQ.data])
  const counts = useMemo(() => {
    const out: Partial<Record<Severity, number>> = {}
    for (const a of list) out[a.severity] = (out[a.severity] ?? 0) + 1
    return out
  }, [list])

  const openCount = openQ.data?.length
  const unacked = openQ.data?.filter((a) => !a.ackedAt).length ?? 0
  const resolvedCount = resolvedQ.data?.length

  // Rule editor (deep-linkable via ?rule=<id>).
  const ruleId = sp.get('rule')
  const editing = useMemo(() => (ruleId ? rulesQ.data?.find((r) => r.id === ruleId) ?? null : null), [ruleId, rulesQ.data])
  const openRule = (r: AlertRule) =>
    setSp(
      (prev) => {
        const p = new URLSearchParams(prev)
        p.set('tab', 'rules')
        p.set('rule', r.id)
        return p
      },
      { replace: true },
    )
  const closeRule = useCallback(
    () =>
      setSp(
        (prev) => {
          const p = new URLSearchParams(prev)
          p.delete('rule')
          return p
        },
        { replace: true },
      ),
    [setSp],
  )
  const [testOpen, setTestOpen] = useState(false)

  const onAck = (a: Alert) =>
    ack.mutate(a.id, {
      // Same id as the stream's toast for this alert (sse.ts) so the two replace each other instead of stacking.
      onSuccess: () => toast.success('Alert acknowledged', a.title, { id: `alert-${a.id}`, duration: 2500 }),
    })
  const onToggle = (rule: AlertRule, patch: Pick<Partial<AlertRule>, 'enabled' | 'notify'>) =>
    saveRule(
      { ...rule, ...patch },
      {
        onSuccess: (saved) =>
          toast.success(
            patch.enabled !== undefined ? (saved.enabled ? 'Rule enabled' : 'Rule disabled') : saved.notify ? 'Notifications on' : 'Notifications off',
            saved.name,
            { duration: 2000 },
          ),
      },
    )

  const deviceName = (id: string) => devicesQ.data?.find((d) => d.id === id)?.name

  const summary = (
    <p className="num" aria-live="polite">
      {source.isPending ? (
        'Loading alerts…'
      ) : hasAlertFilters(filters) ? (
        <>
          <span className="font-medium text-fg">{formatInt(filtered.length)}</span> of {plural(list.length, 'alert')}
        </>
      ) : (
        <span className="font-medium text-fg">{plural(list.length, tab === 'open' ? 'open alert' : 'resolved alert')}</span>
      )}
      {tab === 'open' && !source.isPending && list.length ? <span className="text-fg-muted"> · {unacked ? `${formatInt(unacked)} unacknowledged` : 'all acknowledged'}</span> : null}
    </p>
  )

  return (
    <>
      <PageHeader
        title="Alerts"
        description="Open and resolved watchdog alerts, plus the rules that raise them."
        actions={
          tab === 'rules' ? (
            <>
              <a href="#rule-guide" className={buttonClass({ size: 'sm', variant: 'ghost' })}>
                <BookOpen aria-hidden="true" />
                Rule guide
              </a>
              {isAdmin ? (
                <Button size="sm" leadingIcon={Send} onClick={() => setTestOpen(true)}>
                  Send test notification
                </Button>
              ) : null}
            </>
          ) : (
            <Button size="sm" variant="ghost" leadingIcon={SlidersHorizontal} onClick={() => setTab('rules')}>
              Manage rules
            </Button>
          )
        }
      >
        <Tabs<Tab>
          aria-label="Alert views"
          idPrefix="alerts"
          value={tab}
          onValueChange={setTab}
          tabs={[
            { id: 'open', label: 'Open', icon: Bell, count: openCount },
            { id: 'resolved', label: 'Resolved', icon: BellOff, count: resolvedCount },
            { id: 'rules', label: 'Rules', icon: SlidersHorizontal, count: rulesQ.data?.length },
          ]}
        />
      </PageHeader>

      {tab !== 'rules' ? (
        <div className="space-y-4">
          <AlertsToolbar filters={filters} onChange={patchFilters} onReset={resetFilters} devices={deviceOptions} counts={counts} summary={summary} />
          <TabPanel id={tab} active idPrefix="alerts">
            <AlertList
              state={tab}
              groups={groups}
              loading={source.isPending}
              fetching={source.isFetching && !source.isPending}
              error={source.data ? undefined : source.error}
              onRetry={() => void source.refetch()}
              filtering={hasAlertFilters(filters)}
              onClearFilters={() => setFilters(DEFAULT_ALERT_FILTERS)}
              isAdmin={isAdmin}
              ackingId={ack.isPending ? ack.variables ?? null : null}
              onAck={onAck}
              now={now}
            />
          </TabPanel>
        </div>
      ) : (
        <TabPanel id="rules" active idPrefix="alerts" className="space-y-6">
          <div className="space-y-3">
            <p className="text-xs text-fg-muted">
              {isAdmin ? 'Click a rule to edit it. Switches change a rule immediately.' : 'Click a rule to see its settings. Only admins can change rules.'}
            </p>
            <div className="hidden md:block">
              <RulesTable
                rules={rulesQ.data}
                loading={rulesQ.isPending}
                error={rulesQ.data ? undefined : rulesQ.error}
                onRetry={() => void rulesQ.refetch()}
                isAdmin={isAdmin}
                onEdit={openRule}
                onToggle={onToggle}
                pendingId={pendingId}
                deviceName={deviceName}
              />
            </div>
            <div className="md:hidden">
              {rulesQ.data?.length ? (
                <RuleCards rules={rulesQ.data} isAdmin={isAdmin} onEdit={openRule} onToggle={onToggle} pendingId={pendingId} deviceName={deviceName} />
              ) : (
                <RulesTable
                  rules={rulesQ.data}
                  loading={rulesQ.isPending}
                  error={rulesQ.data ? undefined : rulesQ.error}
                  onRetry={() => void rulesQ.refetch()}
                  isAdmin={isAdmin}
                  onEdit={openRule}
                  onToggle={onToggle}
                  pendingId={pendingId}
                  deviceName={deviceName}
                />
              )}
            </div>
          </div>
          <RuleTypeGuide />
        </TabPanel>
      )}

      <RuleEditorDialog rule={editing} open={!!editing} onClose={closeRule} isAdmin={isAdmin} devices={devicesQ.data ?? []} />
      <TestNotificationDialog open={testOpen} onClose={() => setTestOpen(false)} notifiers={settingsQ.data?.notifiers} />
    </>
  )
}
