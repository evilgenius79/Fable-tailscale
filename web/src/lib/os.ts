import type { LucideIcon } from 'lucide-react'
import {
  Apple,
  Cpu,
  HardDrive,
  Laptop,
  Monitor,
  Server,
  Smartphone,
  Tablet,
  Terminal,
  Tv,
  type LucideProps,
} from 'lucide-react'

export type OSFamily =
  | 'linux'
  | 'macos'
  | 'windows'
  | 'ios'
  | 'android'
  | 'freebsd'
  | 'openbsd'
  | 'tvos'
  | 'other'

export interface OSInfo {
  family: OSFamily
  /** Human label: "macOS", "Linux", "Windows", "iOS", "Android", … */
  label: string
  /** lucide icon representing the platform. */
  icon: LucideIcon
}

const TABLE: Record<OSFamily, { label: string; icon: LucideIcon }> = {
  linux: { label: 'Linux', icon: Terminal },
  macos: { label: 'macOS', icon: Apple },
  windows: { label: 'Windows', icon: Monitor },
  ios: { label: 'iOS', icon: Smartphone },
  android: { label: 'Android', icon: Smartphone },
  freebsd: { label: 'FreeBSD', icon: Server },
  openbsd: { label: 'OpenBSD', icon: Server },
  tvos: { label: 'tvOS', icon: Tv },
  other: { label: 'Unknown', icon: Cpu },
}

/** Normalise a Tailscale `os` string ("linux", "macOS", "windows", "iOS", "android", "tvOS", …) to a family. */
export function osFamily(os: string | null | undefined): OSFamily {
  const s = (os ?? '').trim().toLowerCase()
  if (!s) return 'other'
  if (s.startsWith('ipados') || s === 'ios' || s.startsWith('iphone') || s.startsWith('ipad')) return 'ios'
  if (s === 'macos' || s === 'darwin' || s.startsWith('mac')) return 'macos'
  if (s.startsWith('win')) return 'windows'
  if (s.startsWith('android')) return 'android'
  if (s.startsWith('tvos') || s.includes('appletv')) return 'tvos'
  if (s.startsWith('freebsd')) return 'freebsd'
  if (s.startsWith('openbsd')) return 'openbsd'
  if (s.startsWith('linux') || s.includes('ubuntu') || s.includes('debian') || s.includes('synology') || s.includes('qnap') || s.includes('unraid') || s.includes('raspb')) return 'linux'
  return 'other'
}

/** OS label + icon for a device's `os` string. Unknown values fall back to the raw string. */
export function osInfo(os: string | null | undefined): OSInfo {
  const family = osFamily(os)
  const entry = TABLE[family]
  const raw = (os ?? '').trim()
  return {
    family,
    label: family === 'other' && raw ? raw : entry.label,
    icon: entry.icon,
  }
}

/** Short label suitable for badges/tables. */
export function osLabel(os: string | null | undefined): string {
  return osInfo(os).label
}

/**
 * A device-form icon: uses the OS family plus optional model/tags hints so a
 * NAS, a phone or a server render as what they are rather than by OS only.
 */
export function deviceIcon(input: {
  os?: string | null
  deviceModel?: string | null
  tags?: string[] | null
  hostname?: string | null
}): LucideIcon {
  const fam = osFamily(input.os)
  const model = (input.deviceModel ?? '').toLowerCase()
  const host = (input.hostname ?? '').toLowerCase()
  const tags = input.tags ?? []
  if (fam === 'ios' || fam === 'android') {
    return model.includes('ipad') || model.includes('tablet') ? Tablet : Smartphone
  }
  if (fam === 'tvos') return Tv
  if (model.includes('synology') || model.includes('nas') || host.includes('nas') || model.includes('qnap')) return HardDrive
  if (tags.some((t) => /server|exit|router|ci|infra/.test(t)) || model.includes('server') || host.includes('server') || host.includes('vm')) return Server
  if (fam === 'macos' || model.includes('macbook') || model.includes('thinkpad') || model.includes('laptop')) return Laptop
  if (fam === 'windows') return Laptop
  if (fam === 'linux') return model.includes('raspberry') || host.startsWith('pi') ? Cpu : Server
  return Monitor
}

export type { LucideProps }
