import { useState } from 'react'
import { ChevronDown, KeyRound, Pencil, Radar, Route, Settings2, ShieldCheck, Tags, Trash2 } from 'lucide-react'
import { useAuthorize, usePing, useSetKeyExpiry } from '../../api/hooks'
import type { Device } from '../../api/types'
import { Button } from '../ui/Button'
import { DropdownMenu, type MenuItem } from '../ui/Menu'
import { toast } from '../ui/Toast'
import { pingResultToast } from './deviceDetail'
import { DeviceDialogs, type DeviceDialogKind } from './DeviceDialogs'

export interface DeviceActionsProps {
  device: Device
  /** Identity is admin AND the hub has admin actions enabled. */
  canManage: boolean
  /** Open a specific dialog from elsewhere on the page (e.g. the Routes card). */
  requestedDialog?: DeviceDialogKind | null
  onDialogHandled?: () => void
}

/** Ping button (viewer) plus the admin "Manage" menu and its dialogs. */
export function DeviceActions({ device, canManage, requestedDialog = null, onDialogHandled }: DeviceActionsProps) {
  const ping = usePing()
  const authorize = useAuthorize()
  const keyExpiry = useSetKeyExpiry()
  const [dialog, setDialog] = useState<DeviceDialogKind | null>(null)
  const open = requestedDialog ?? dialog
  const close = () => {
    setDialog(null)
    onDialogHandled?.()
  }

  // Ping results go to a toast (one per device, replaced on re-ping) rather than
  // into the header's actions column: a chip there widens that column and makes
  // the meta row (IPv6, tags) wrap.
  const runPing = () =>
    ping.mutate(device.id, {
      onSuccess: (r) => toast({ ...pingResultToast(device, r), duration: r.error ? 8000 : 6000 }),
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
      {canManage ? <DeviceDialogs kind={open} device={device} onClose={close} /> : null}
    </>
  )
}
