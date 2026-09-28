import { describe, expect, it } from 'vitest'
import { Apple, Cpu, HardDrive, Laptop, Server, Smartphone, Tablet, Terminal, Tv } from 'lucide-react'
import { deviceIcon, osFamily, osInfo, osLabel } from './os'

describe('osFamily', () => {
  it('normalises Tailscale os strings', () => {
    expect(osFamily('linux')).toBe('linux')
    expect(osFamily('Linux')).toBe('linux')
    expect(osFamily('macOS')).toBe('macos')
    expect(osFamily('darwin')).toBe('macos')
    expect(osFamily('windows')).toBe('windows')
    expect(osFamily('Windows 11')).toBe('windows')
    expect(osFamily('iOS')).toBe('ios')
    expect(osFamily('iPadOS')).toBe('ios')
    expect(osFamily('android')).toBe('android')
    expect(osFamily('tvOS')).toBe('tvos')
    expect(osFamily('freebsd')).toBe('freebsd')
    expect(osFamily('openbsd')).toBe('openbsd')
    expect(osFamily('synology')).toBe('linux')
  })
  it('falls back to other', () => {
    expect(osFamily('')).toBe('other')
    expect(osFamily(undefined)).toBe('other')
    expect(osFamily('plan9')).toBe('other')
  })
})

describe('osInfo / osLabel', () => {
  it('gives labels and icons', () => {
    expect(osInfo('macOS')).toMatchObject({ family: 'macos', label: 'macOS', icon: Apple })
    expect(osInfo('linux').icon).toBe(Terminal)
    expect(osInfo('iOS').icon).toBe(Smartphone)
    expect(osInfo('tvOS').icon).toBe(Tv)
    expect(osLabel('windows')).toBe('Windows')
  })
  it('keeps the raw string for unknown platforms', () => {
    expect(osInfo('plan9').label).toBe('plan9')
    expect(osInfo('plan9').icon).toBe(Cpu)
    expect(osInfo(null).label).toBe('Unknown')
  })
})

describe('deviceIcon', () => {
  it('uses form hints before OS', () => {
    expect(deviceIcon({ os: 'linux', deviceModel: 'Synology DS923+' })).toBe(HardDrive)
    expect(deviceIcon({ os: 'linux', tags: ['tag:server'] })).toBe(Server)
    expect(deviceIcon({ os: 'linux', deviceModel: 'Raspberry Pi 4' })).toBe(Cpu)
    expect(deviceIcon({ os: 'linux', hostname: 'pi-garage' })).toBe(Cpu)
    expect(deviceIcon({ os: 'linux' })).toBe(Server)
    expect(deviceIcon({ os: 'macOS' })).toBe(Laptop)
    expect(deviceIcon({ os: 'windows' })).toBe(Laptop)
    expect(deviceIcon({ os: 'iOS' })).toBe(Smartphone)
    expect(deviceIcon({ os: 'iPadOS', deviceModel: 'iPad Pro' })).toBe(Tablet)
    expect(deviceIcon({ os: 'tvOS' })).toBe(Tv)
  })
})
