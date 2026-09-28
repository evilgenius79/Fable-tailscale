import { useState } from 'react'
import { ChevronDown, KeyRound, Pencil, Radar, Route, Settings2, ShieldCheck, Tags, Trash2, X } from 'lucide-react'
import { useAuthorize, usePing, useSetKeyExpiry } from '../../api/hooks'
import type { Device, PingResult } from '../../api/types'
import { cn } from '../../lib/cn'
import { formatLatency, formatTime } from '../../lib/format'
import { pathLabel, pathTone } from '../../lib/status'
import { Badge } from '../ui/Badge'
import { Button, IconButton } from '../ui/Button'
import { DropdownMenu, type MenuItem } from '../ui/Menu'
import { toast } from '../ui/Toast'
import { DeviceDialogs, type DeviceDialogKind } from './DeviceDialogs'

export interface DeviceActionsProps {
  device: Device
  /** Identity is admin AND the hub has admin actions enabled. */
  canManage: boolean
  /** Open a specific dialog from elsewhere on the page (e.g. the Routes card). */
  requestedDialog?: DeviceDialogKind | null
  onDialogHandled?: () => void
}

function PingResultChip({ result, onDismiss }: { result: PingResult; onDismiss: () => void }) {
  const failed = !!result.error
  return (
    <div
      role="status"
      aria-live="polite"
      className={cn(
        'flex min-w-0 items-center gap-2 rounded-md border px-2.5 py-1.5 text-xs animate-fade-in',
        failed ? 'border-critical/30 bg-critical-soft' : 'border-border bg-surface-raised shadow-xs',
      )}
    >
      <Radar className={cn('size-3.5 shrink-0', failed ? 'text-critical' : 'text-fg-muted')} aria-hidden="true" />
      {failed ? (
        <span className="min-w-0 truncate text-fg">
          Ping failed <span className="text-fg-muted">· {result.error}</span>
        </span>
      ) : (
        <span className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
          <span className="num font-semibold text-fg">{formatLatency(result.latencyMs)}</span>
          <Badge size="sm" tone={pathTone(result.path)} dot>
            {pathLabel(result.path, result.relay)}
          </Badge>
          {result.endpoint ? <span className="truncate font-mono text-[11px] text-fg-secondary">{result.endpoint}</span> : null}
          <span className="text-fg-muted num">{formatTime(result.at, true)}</span>
        </span>
      )}
      <IconButton icon={X} label="Dismiss ping result" size="xs" onClick={onDismiss} tooltip={false} className="ml-auto" />
    </div>
  )
}

/** Ping button (viewer) plus the admin "Manage" menu and its dialogs. */
export function DeviceActions({ device, canManage, requestedDialog = null, onDialogHandled }: DeviceActionsProps) {
  const ping = usePing()
  const authorize = useAuthorize()
  const keyExpiry = useSetKeyExpiry()
  const [result, setResult] = useState<PingResult | null>(null)
  const [dialog, setDialog] = useState<DeviceDialogKind | null>(null)
  const open = requestedDialog ?? dialog
  const close = () => {
    setDialog(null)
    onDialogHandled?.()
  }

  const runPing = () =>
    ping.mutate(device.id, {
      onSuccess: (r) => setResult(r),
    })

  const items: MenuItem[] = []
  if (!device.authorized) {
    items.push({
      id: 'authorize',
      label: 'Authorize device',
      icon: ShieldCheck,
      onSelect: () => authorize.mutate({ id: device.id }, { onSuccess: () => toast.success('Device authorized', `${device.name} can now join the tailnet.`) }),
    })
  }
  items.push({ id: 'tags', label: 'Edit tags…', icon: Tags, onSelect: () => setDialog('tags') })
  items.push({
    id: 'key',
    label: device.keyExpiryDisabled ? 'Enable key expiry' : 'Disable key expiry',
    icon: KeyRound,
    onSelect: () =>
      keyExpiry.mutate(
        { id: device.id, disabled: !device.keyExpiryDisabled },
        { onSuccess: (d) => toast.success(d.keyExpiryDisabled ? 'Key expiry disabled' : 'Key expiry enabled', d.name) },
      ),
  })
  items.push({ id: 'routes', label: 'Approve routes…', icon: Route, onSelect: () => setDialog('routes'), disabled: !device.advertisedRoutes.length && !device.enabledRoutes.length })
  items.push({ id: 'rename', label: 'Rename…', icon: Pencil, onSelect: () => setDialog('rename') })
  items.push({ id: 'delete', label: 'Remove from tailnet…', icon: Trash2, danger: true, separatorBefore: true, disabled: device.isSelf, onSelect: () => setDialog('delete') })

  const busy = authorize.isPending || keyExpiry.isPending

  return (
    <>
      <Button size="sm" leadingIcon={Radar} onClick={runPing} loading={ping.isPending} aria-label={`Ping ${device.name}`}>
        Ping
      </Button>
      {canManage ? (
        <DropdownMenu
          label="Manage device"
          align="end"
          width={236}
          items={items}
          trigger={
            <Button size="sm" variant="primary" leadingIcon={Settings2} trailingIcon={ChevronDown} loading={busy}>
              Manage
            </Button>
          }
        />
      ) : null}
      {result ? (
        <div className="basis-full sm:flex sm:justify-end">
          <PingResultChip result={result} onDismiss={() => setResult(null)} />
        </div>
      ) : null}
      {canManage ? <DeviceDialogs kind={open} device={device} onClose={close} /> : null}
    </>
  )
}
