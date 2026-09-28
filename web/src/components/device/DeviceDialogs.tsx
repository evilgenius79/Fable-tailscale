import { useEffect, useMemo, useRef, useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { Plus } from 'lucide-react'
import { useDeleteDevice, useRename, useSetRoutes, useSetTags } from '../../api/hooks'
import type { Device } from '../../api/types'
import { cn } from '../../lib/cn'
import { plural, shortDnsName } from '../../lib/format'
import { Badge } from '../ui/Badge'
import { Button } from '../ui/Button'
import { Dialog } from '../ui/Dialog'
import { Field, Input } from '../ui/Input'
import { Tag } from '../ui/Tag'
import { toast } from '../ui/Toast'
import { normalizeTag, routeRows, validateDeviceName, validateTag } from './deviceDetail'

export type DeviceDialogKind = 'tags' | 'routes' | 'rename' | 'delete'

interface BaseProps {
  open: boolean
  onClose: () => void
  device: Device
}

// ---------------------------------------------------------------------------
// Tags
// ---------------------------------------------------------------------------

export function TagsDialog({ open, onClose, device }: BaseProps) {
  const [tags, setTags] = useState<string[]>(device.tags)
  const [draft, setDraft] = useState('')
  const [error, setError] = useState<string | null>(null)
  const inputRef = useRef<HTMLInputElement>(null)
  const mutation = useSetTags()

  useEffect(() => {
    if (open) {
      setTags(device.tags)
      setDraft('')
      setError(null)
    }
  }, [open, device.tags])

  const add = () => {
    const t = normalizeTag(draft)
    const err = validateTag(t, tags)
    if (err) {
      setError(err)
      return
    }
    setTags((prev) => [...prev, t])
    setDraft('')
    setError(null)
  }

  const changed = tags.length !== device.tags.length || tags.some((t) => !device.tags.includes(t))

  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (draft.trim()) {
      add()
      return
    }
    if (!changed) return
    mutation.mutate(
      { id: device.id, tags },
      {
        onSuccess: () => {
          toast.success('Tags updated', tags.length ? tags.join(', ') : 'All tags removed')
          onClose()
        },
      },
    )
  }

  return (
    <Dialog
      open={open}
      onClose={onClose}
      title="Edit tags"
      description="ACL tags replace the device's user as its identity. Changes are applied through the Tailscale control API."
      initialFocus={inputRef}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={mutation.isPending}>
            Cancel
          </Button>
          <Button variant="primary" form="tags-form" type="submit" loading={mutation.isPending} disabled={!changed && !draft.trim()}>
            Save tags
          </Button>
        </>
      }
    >
      <form id="tags-form" onSubmit={submit} className="space-y-4">
        <div className="flex min-h-9 flex-wrap items-center gap-1.5 rounded-md border border-border bg-surface-inset p-2" aria-live="polite">
          {tags.length ? tags.map((t) => <Tag key={t} tag={t} onRemove={() => setTags((prev) => prev.filter((x) => x !== t))} />) : <span className="px-1 text-xs text-fg-faint">No tags</span>}
        </div>
        <Field label="Add a tag" hint="Letters, digits and dashes. The tag: prefix is added for you." error={error}>
          {({ id, describedBy }) => (
            <div className="flex items-center gap-2">
              <Input
                ref={inputRef}
                id={id}
                aria-describedby={describedBy}
                value={draft}
                onChange={(e) => {
                  setDraft(e.target.value)
                  if (error) setError(null)
                }}
                placeholder="tag:server"
                mono
                invalid={!!error}
                autoComplete="off"
                spellCheck={false}
                className="flex-1"
              />
              <Button type="button" onClick={add} leadingIcon={Plus} disabled={!draft.trim()}>
                Add
              </Button>
            </div>
          )}
        </Field>
      </form>
    </Dialog>
  )
}

// ---------------------------------------------------------------------------
// Routes
// ---------------------------------------------------------------------------

export function RoutesDialog({ open, onClose, device }: BaseProps) {
  const rows = useMemo(() => routeRows(device), [device])
  const [enabled, setEnabled] = useState<Set<string>>(() => new Set(device.enabledRoutes))
  const mutation = useSetRoutes()

  useEffect(() => {
    if (open) setEnabled(new Set(device.enabledRoutes))
  }, [open, device.enabledRoutes])

  const toggle = (route: string) =>
    setEnabled((prev) => {
      const next = new Set(prev)
      if (next.has(route)) next.delete(route)
      else next.add(route)
      return next
    })

  const list = Array.from(enabled)
  const changed = list.length !== device.enabledRoutes.length || list.some((r) => !device.enabledRoutes.includes(r))

  const save = () =>
    mutation.mutate(
      { id: device.id, routes: rows.filter((r) => enabled.has(r.route)).map((r) => r.route) },
      {
        onSuccess: (d) => {
          toast.success('Routes updated', d.enabledRoutes.length ? `${plural(d.enabledRoutes.length, 'route')} enabled` : 'No routes enabled')
          onClose()
        },
      },
    )

  return (
    <Dialog
      open={open}
      onClose={onClose}
      title="Approve routes"
      description="Choose which advertised subnet routes and exit-node routes other devices may use."
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={mutation.isPending}>
            Cancel
          </Button>
          <Button variant="primary" onClick={save} loading={mutation.isPending} disabled={!changed}>
            Save routes
          </Button>
        </>
      }
    >
      {rows.length ? (
        <ul className="divide-y divide-border-subtle rounded-md border border-border">
          {rows.map((r) => {
            const id = `route-${r.route.replace(/[^a-z0-9]/gi, '-')}`
            return (
              <li key={r.route} className="flex items-center gap-3 px-3 py-2.5">
                <input id={id} type="checkbox" checked={enabled.has(r.route)} onChange={() => toggle(r.route)} className="size-4 shrink-0 accent-[var(--accent)] focus-ring" />
                <label htmlFor={id} className="flex min-w-0 flex-1 cursor-pointer items-center gap-2">
                  <span className="truncate font-mono text-xs text-fg">{r.route}</span>
                  {r.exit ? (
                    <Badge size="sm" tone="info">
                      Exit node
                    </Badge>
                  ) : null}
                  {r.primary ? (
                    <Badge size="sm" tone="neutral">
                      Primary
                    </Badge>
                  ) : null}
                  {!device.advertisedRoutes.includes(r.route) ? (
                    <Badge size="sm" tone="warning">
                      Not advertised
                    </Badge>
                  ) : null}
                </label>
              </li>
            )
          })}
        </ul>
      ) : (
        <p className="text-sm text-fg-muted">This device does not advertise any routes.</p>
      )}
    </Dialog>
  )
}

// ---------------------------------------------------------------------------
// Rename
// ---------------------------------------------------------------------------

export function RenameDialog({ open, onClose, device }: BaseProps) {
  const [name, setName] = useState(device.name)
  const [touched, setTouched] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)
  const mutation = useRename()
  const suffix = device.dnsName.includes('.') ? device.dnsName.slice(device.dnsName.indexOf('.')) : ''

  useEffect(() => {
    if (open) {
      setName(device.name)
      setTouched(false)
    }
  }, [open, device.name])

  const error = validateDeviceName(name, device.name)
  const trimmed = name.trim().toLowerCase()
  const submit = (e: FormEvent) => {
    e.preventDefault()
    setTouched(true)
    if (error) return
    mutation.mutate(
      { id: device.id, name: trimmed },
      {
        onSuccess: (d) => {
          toast.success('Device renamed', d.dnsName)
          onClose()
        },
      },
    )
  }

  return (
    <Dialog
      open={open}
      onClose={onClose}
      title="Rename device"
      description="Sets the machine name used for MagicDNS. Existing connections keep working."
      initialFocus={inputRef}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={mutation.isPending}>
            Cancel
          </Button>
          <Button variant="primary" form="rename-form" type="submit" loading={mutation.isPending} disabled={!!error}>
            Rename
          </Button>
        </>
      }
    >
      <form id="rename-form" onSubmit={submit} className="space-y-3">
        <Field
          label="Machine name"
          error={touched && error ? error : undefined}
          hint={
            <>
              Will resolve as <span className="font-mono text-fg">{(trimmed || shortDnsName(device.dnsName)) + suffix}</span>
            </>
          }
        >
          {({ id, describedBy }) => (
            <Input
              ref={inputRef}
              id={id}
              aria-describedby={describedBy}
              value={name}
              onChange={(e) => setName(e.target.value)}
              onBlur={() => setTouched(true)}
              mono
              invalid={touched && !!error}
              autoComplete="off"
              spellCheck={false}
              maxLength={63}
            />
          )}
        </Field>
      </form>
    </Dialog>
  )
}

// ---------------------------------------------------------------------------
// Delete
// ---------------------------------------------------------------------------

export function DeleteDialog({ open, onClose, device }: BaseProps) {
  const [confirm, setConfirm] = useState('')
  const cancelRef = useRef<HTMLButtonElement>(null)
  const navigate = useNavigate()
  const mutation = useDeleteDevice()

  useEffect(() => {
    if (open) setConfirm('')
  }, [open])

  const matches = confirm.trim() === device.name
  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (!matches) return
    mutation.mutate(device.id, {
      onSuccess: () => {
        toast.success('Device removed', `${device.name} was removed from the tailnet.`)
        onClose()
        navigate('/devices', { replace: true })
      },
    })
  }

  return (
    <Dialog
      open={open}
      onClose={onClose}
      title="Remove device from tailnet"
      description="The node key is revoked and the device disconnects immediately. It can re-join with a fresh login."
      initialFocus={cancelRef}
      footer={
        <>
          <Button ref={cancelRef} variant="ghost" onClick={onClose} disabled={mutation.isPending}>
            Cancel
          </Button>
          <Button variant="danger" form="delete-form" type="submit" loading={mutation.isPending} disabled={!matches}>
            Remove device
          </Button>
        </>
      }
    >
      <form id="delete-form" onSubmit={submit} className="space-y-3">
        <div className="rounded-md border border-critical/30 bg-critical-soft px-3 py-2 text-sm text-fg">
          This cannot be undone. Alert history and metrics for <span className="font-semibold">{device.name}</span> are kept.
        </div>
        <Field
          label={
            <>
              Type <span className="font-mono">{device.name}</span> to confirm
            </>
          }
        >
          {({ id }) => <Input id={id} value={confirm} onChange={(e) => setConfirm(e.target.value)} mono autoComplete="off" spellCheck={false} className={cn(matches && 'text-online')} />}
        </Field>
      </form>
    </Dialog>
  )
}

/** Renders whichever dialog is open (or nothing). */
export function DeviceDialogs({ kind, device, onClose }: { kind: DeviceDialogKind | null; device: Device; onClose: () => void }) {
  return (
    <>
      <TagsDialog open={kind === 'tags'} onClose={onClose} device={device} />
      <RoutesDialog open={kind === 'routes'} onClose={onClose} device={device} />
      <RenameDialog open={kind === 'rename'} onClose={onClose} device={device} />
      <DeleteDialog open={kind === 'delete'} onClose={onClose} device={device} />
    </>
  )
}
