import { FlaskConical, RefreshCw } from 'lucide-react'
import { useAudit, useDevices, useIsAdmin, useMe, useOverview, useRefresh, useSettings } from '../api/hooks'
import { AboutSection } from '../components/settings/AboutSection'
import { AgentInstallSection } from '../components/settings/AgentInstallSection'
import { AuditSection } from '../components/settings/AuditSection'
import { AuthSection } from '../components/settings/AuthSection'
import { DataSourcesSection } from '../components/settings/DataSourcesSection'
import { HubSection } from '../components/settings/HubSection'
import { NotificationsSection } from '../components/settings/NotificationsSection'
import { SettingsNav } from '../components/settings/SettingsSection'
import { StorageSection } from '../components/settings/StorageSection'
import { visibleSections } from '../components/settings/settings'
import { Badge } from '../components/ui/Badge'
import { Button } from '../components/ui/Button'
import { ErrorState } from '../components/ui/ErrorState'
import { PageHeader } from '../components/ui/PageHeader'
import { toast } from '../components/ui/Toast'
import { useUIStore } from '../store'

export default function SettingsPage() {
  const settings = useSettings()
  const ov = useOverview()
  const me = useMe()
  const isAdmin = useIsAdmin()
  const devices = useDevices()
  const audit = useAudit(100, { enabled: isAdmin })
  const refresh = useRefresh()
  const hubFromStream = useUIStore((s) => s.hub)
  const hub = ov.data?.hub ?? hubFromStream
  const loading = settings.isPending
  const demo = settings.data?.demoMode ?? hub?.demoMode ?? false
  const sections = visibleSections(isAdmin)
  const deviceName = (id: string) => devices.data?.find((d) => d.id === id)?.name

  return (
    <>
      <PageHeader
        title="Settings"
        description="Hub configuration, access control, retention, notifiers and the audit log. Read-only: change values with flags or environment variables on the hub."
        meta={
          hub ? (
            <>
              <span>
                Tailwatch <span className="font-mono text-fg">{hub.version}</span>
              </span>
              <span aria-hidden="true">·</span>
              <span className="font-medium text-fg">{hub.tailnet}</span>
              {me.data ? (
                <>
                  <span aria-hidden="true">·</span>
                  <span>
                    you are <span className="font-medium text-fg">{me.data.role}</span>
                  </span>
                </>
              ) : null}
              {demo ? (
                <Badge size="sm" tone="info">
                  Demo data
                </Badge>
              ) : null}
            </>
          ) : null
        }
        actions={
          isAdmin ? (
            <Button
              size="sm"
              leadingIcon={RefreshCw}
              loading={refresh.isPending}
              onClick={() =>
                refresh.mutate(undefined, {
                  onSuccess: () => toast.info('Refresh requested', 'The hub is polling the tailnet now.', { duration: 2500 }),
                })
              }
            >
              Refresh now
            </Button>
          ) : null
        }
      />

      {demo ? (
        <div role="status" className="mb-5 flex items-start gap-3 rounded-lg border border-info/30 bg-info-soft px-3.5 py-3 text-sm shadow-xs">
          <FlaskConical className="mt-0.5 size-4 shrink-0 text-info" aria-hidden="true" />
          <div className="min-w-0">
            <p className="font-medium text-fg">Demo mode</p>
            <p className="text-[13px] text-fg-secondary">
              The hub is running with <span className="font-mono text-xs">--demo</span>: devices, metrics and events are simulated, authentication is off and every visitor is an admin. Nothing here reflects a real tailnet.
            </p>
          </div>
        </div>
      ) : null}

      {settings.error && !settings.data ? <ErrorState error={settings.error} onRetry={() => void settings.refetch()} retrying={settings.isFetching} className="mb-6" /> : null}

      <div className="lg:grid lg:grid-cols-[180px_minmax(0,1fr)] lg:gap-8 xl:grid-cols-[200px_minmax(0,1fr)]">
        <SettingsNav sections={sections} className="mb-4 lg:mb-0" />
        <div className="min-w-0 space-y-5 xl:space-y-6">
          <HubSection hub={hub} settings={settings.data} loading={loading && ov.isPending} />
          <AuthSection settings={settings.data} me={me.data} loading={loading} />
          <DataSourcesSection settings={settings.data} hub={hub} loading={loading} />
          <StorageSection settings={settings.data} loading={loading} />
          <NotificationsSection settings={settings.data} loading={loading} isAdmin={isAdmin} />
          <AgentInstallSection settings={settings.data} hub={hub} me={me.data} />
          {isAdmin ? <AuditSection entries={audit.data} loading={audit.isPending} error={audit.data ? undefined : audit.error} onRetry={() => void audit.refetch()} deviceName={deviceName} /> : null}
          <AboutSection hub={hub} />
        </div>
      </div>
    </>
  )
}
