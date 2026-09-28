import { useEffect, useId, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useNavigate } from 'react-router-dom'
import { CornerDownLeft, Search } from 'lucide-react'
import type { Device } from '../../api/types'
import { useDevices } from '../../api/hooks'
import { cn } from '../../lib/cn'
import { osInfo } from '../../lib/os'
import { deviceStatus } from '../../lib/status'
import { useFocusTrap } from '../../lib/useFocusTrap'
import { useUIStore } from '../../store'
import { Kbd } from '../ui/Kbd'
import { StatusDot } from '../ui/StatusDot'
import { NAV_ITEMS } from './Sidebar'

type Result = { kind: 'page'; id: string; label: string; to: string; icon: (typeof NAV_ITEMS)[number]['icon'] } | { kind: 'device'; id: string; device: Device; score: number }

/** Simple ranked substring match across name, dns, addresses, user, os, tags. Exported for tests. */
export function scoreDevice(d: Device, q: string): number {
  if (!q) return 1
  const s = q.toLowerCase()
  const name = d.name.toLowerCase()
  if (name === s) return 100
  if (name.startsWith(s)) return 80
  if (name.includes(s)) return 60
  if (d.hostname.toLowerCase().includes(s) || d.dnsName.toLowerCase().includes(s)) return 50
  if (d.addresses.some((a) => a.includes(s))) return 45
  if (d.tags.some((t) => t.toLowerCase().includes(s))) return 35
  if ((d.userDisplayName ?? '').toLowerCase().includes(s) || d.user.toLowerCase().includes(s)) return 30
  if (d.os.toLowerCase().includes(s) || (d.deviceModel ?? '').toLowerCase().includes(s)) return 25
  return 0
}

/** Cmd/Ctrl+K palette: jumps to a device or a page. Mounted once by AppShell. */
export function CommandPalette() {
  const open = useUIStore((s) => s.paletteOpen)
  const setOpen = useUIStore((s) => s.setPaletteOpen)
  const navigate = useNavigate()
  const [q, setQ] = useState('')
  const [active, setActive] = useState(0)
  const inputRef = useRef<HTMLInputElement>(null)
  const listRef = useRef<HTMLUListElement>(null)
  const panelRef = useRef<HTMLDivElement>(null)
  const id = useId()
  const { data: devices, isPending } = useDevices()
  // aria-modal: Tab must not reach the page behind the overlay (options are not focusable, so focus stays in the combobox).
  useFocusTrap(panelRef, open)

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault()
        setOpen(!useUIStore.getState().paletteOpen)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [setOpen])

  useEffect(() => {
    if (!open) return
    setQ('')
    setActive(0)
    const prev = document.activeElement
    const raf = requestAnimationFrame(() => inputRef.current?.focus())
    const prevOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      cancelAnimationFrame(raf)
      document.body.style.overflow = prevOverflow
      if (prev instanceof HTMLElement && document.contains(prev)) prev.focus()
    }
  }, [open])

  const results = useMemo<Result[]>(() => {
    const s = q.trim().toLowerCase()
    const pages: Result[] = NAV_ITEMS.filter((n) => !s || n.label.toLowerCase().includes(s)).map((n) => ({ kind: 'page', id: `page:${n.to}`, label: n.label, to: n.to, icon: n.icon }))
    const devs: Result[] = (devices ?? [])
      .map((d) => ({ kind: 'device' as const, id: `dev:${d.id}`, device: d, score: scoreDevice(d, s) }))
      .filter((r) => r.score > 0)
      .sort((a, b) => b.score - a.score || a.device.name.localeCompare(b.device.name))
      .slice(0, s ? 12 : 8)
    return s ? [...devs, ...pages] : [...pages, ...devs]
  }, [q, devices])

  useEffect(() => setActive(0), [results.length, q])

  useEffect(() => {
    const el = listRef.current?.querySelector<HTMLElement>(`[data-index="${active}"]`)
    el?.scrollIntoView({ block: 'nearest' })
  }, [active])

  if (!open) return null

  const select = (r: Result) => {
    setOpen(false)
    navigate(r.kind === 'page' ? r.to : `/devices/${encodeURIComponent(r.device.id)}`)
  }

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      setActive((i) => Math.min(results.length - 1, i + 1))
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      setActive((i) => Math.max(0, i - 1))
    } else if (e.key === 'Enter') {
      e.preventDefault()
      const r = results[active]
      if (r) select(r)
    } else if (e.key === 'Escape') {
      e.preventDefault()
      setOpen(false)
    }
  }

  let lastKind: Result['kind'] | null = null

  return createPortal(
    <div className="fixed inset-0 z-[95] flex items-start justify-center p-4 pt-[12vh] sm:pt-[18vh]" role="presentation">
      <div className="absolute inset-0 bg-overlay animate-fade-in backdrop-blur-[2px]" onClick={() => setOpen(false)} aria-hidden="true" />
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-label="Search"
        className="relative flex w-full max-w-xl flex-col overflow-hidden rounded-xl border border-border bg-surface-raised shadow-overlay animate-scale-in"
        onKeyDown={onKeyDown}
      >
        <div className="flex items-center gap-2.5 border-b border-border px-3">
          <Search className="size-4 shrink-0 text-fg-muted" aria-hidden="true" />
          <input
            ref={inputRef}
            role="combobox"
            aria-expanded="true"
            aria-controls={`${id}-list`}
            aria-activedescendant={results[active] ? `${id}-${results[active].id}` : undefined}
            aria-autocomplete="list"
            autoComplete="off"
            spellCheck={false}
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder="Search devices, pages…"
            className="h-12 w-full bg-transparent text-sm text-fg placeholder:text-fg-faint focus:outline-none"
          />
          <Kbd className="hidden sm:inline-flex">Esc</Kbd>
        </div>
        <ul ref={listRef} id={`${id}-list`} role="listbox" className="max-h-[50vh] overflow-y-auto p-1.5 scrollbar-thin">
          {results.length === 0 ? (
            <li className="px-3 py-8 text-center text-sm text-fg-muted">{isPending ? 'Loading devices…' : `No results for “${q}”`}</li>
          ) : null}
          {results.map((r, i) => {
            const heading = r.kind !== lastKind
            lastKind = r.kind
            const selected = i === active
            return (
              <li key={r.id} role="presentation">
                {heading ? <p className="px-2 pb-1 pt-2 text-[11px] font-semibold uppercase tracking-wider text-fg-muted">{r.kind === 'page' ? 'Pages' : 'Devices'}</p> : null}
                <div
                  id={`${id}-${r.id}`}
                  role="option"
                  aria-selected={selected}
                  data-index={i}
                  onMouseMove={() => setActive(i)}
                  onClick={() => select(r)}
                  className={cn('flex cursor-pointer items-center gap-3 rounded-md px-2.5 py-2 text-sm', selected ? 'bg-surface-active text-fg' : 'text-fg-secondary')}
                >
                  {r.kind === 'page' ? (
                    <>
                      <r.icon className="size-4 text-fg-muted" aria-hidden="true" />
                      <span className="flex-1 text-fg">{r.label}</span>
                    </>
                  ) : (
                    <DeviceRow device={r.device} />
                  )}
                  {selected ? <CornerDownLeft className="size-3.5 text-fg-faint" aria-hidden="true" /> : null}
                </div>
              </li>
            )
          })}
        </ul>
        <div className="flex items-center gap-3 border-t border-border px-3 py-2 text-[11px] text-fg-muted">
          <span className="flex items-center gap-1">
            <Kbd>↑</Kbd>
            <Kbd>↓</Kbd> navigate
          </span>
          <span className="flex items-center gap-1">
            <Kbd>↵</Kbd> open
          </span>
        </div>
      </div>
    </div>,
    document.body,
  )
}

function DeviceRow({ device }: { device: Device }) {
  const st = deviceStatus(device)
  const os = osInfo(device.os)
  return (
    <>
      <StatusDot tone={st.tone} label={st.label} />
      <span className="min-w-0 flex-1">
        <span className="flex items-center gap-2">
          <span className="truncate font-medium text-fg">{device.name}</span>
          <span className="hidden truncate text-xs text-fg-muted sm:inline">{device.addresses[0]}</span>
        </span>
      </span>
      <span className="hidden items-center gap-1 text-xs text-fg-muted sm:flex">
        <os.icon className="size-3.5" aria-hidden="true" />
        {os.label}
      </span>
    </>
  )
}
