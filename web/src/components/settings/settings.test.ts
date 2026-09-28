import { describe, expect, it } from 'vitest'
import {
  SETTINGS_SECTIONS,
  agentFlags,
  agentInstallSnippets,
  auditActionLabel,
  auditTargetLink,
  authModeLabel,
  backendStateTone,
  formatDetails,
  formatIntervalSeconds,
  formatRetentionDays,
  formatRetentionHours,
  hubHealthLabel,
  hubHealthTone,
  hubTagFor,
  notifierMeta,
  principalsLabel,
  storeSegments,
  totalRows,
  visibleSections,
} from './settings'

describe('storage', () => {
  const store = { devices: 3, samples: 100, rollups: 50, events: 20, alerts: 5, sizeBytes: 1024 }
  it('builds fixed-order segments and totals', () => {
    expect(storeSegments(store).map((s) => [s.key, s.value])).toEqual([
      ['samples', 100],
      ['rollups', 50],
      ['events', 20],
      ['alerts', 5],
    ])
    expect(totalRows(store)).toBe(175)
    expect(totalRows(undefined)).toBe(0)
  })
  it('formats retention', () => {
    expect(formatRetentionHours(48)).toBe('2 days')
    expect(formatRetentionHours(24)).toBe('1 day')
    expect(formatRetentionHours(36)).toBe('1d 12h')
    expect(formatRetentionHours(0)).toBe('—')
    expect(formatRetentionDays(90)).toBe('90 days')
    expect(formatRetentionDays(1)).toBe('1 day')
    expect(formatRetentionDays(undefined)).toBe('—')
  })
  it('formats intervals', () => {
    expect(formatIntervalSeconds(15)).toBe('15s')
    expect(formatIntervalSeconds(60)).toBe('1m')
    expect(formatIntervalSeconds(0)).toBe('off')
    expect(formatIntervalSeconds(null)).toBe('—')
  })
})

describe('hub / auth', () => {
  it('labels auth modes and principals', () => {
    expect(authModeLabel('tailscale')).toMatch(/WhoIs/)
    expect(authModeLabel('none')).toMatch(/open/i)
    expect(authModeLabel(undefined)).toBe('—')
    expect(principalsLabel(['*'])).toEqual({ kind: 'everyone', items: [] })
    expect(principalsLabel([])).toEqual({ kind: 'nobody', items: [] })
    expect(principalsLabel([' a@x ', ''])).toEqual({ kind: 'list', items: ['a@x'] })
  })
  it('tones backend state and health', () => {
    expect(backendStateTone('Running')).toBe('online')
    expect(backendStateTone('NeedsLogin')).toBe('warning')
    expect(backendStateTone('Stopped')).toBe('offline')
    expect(backendStateTone(undefined)).toBe('neutral')
    expect(hubHealthTone({ health: [], lastError: undefined })).toBe('online')
    expect(hubHealthTone({ health: ['x'] })).toBe('warning')
    expect(hubHealthTone({ health: [], lastError: 'boom' })).toBe('critical')
    expect(hubHealthLabel({ health: ['a', 'b'] })).toBe('2 warnings')
    expect(hubHealthLabel(null)).toBe('Unknown')
  })
  it('picks the hub tag', () => {
    expect(hubTagFor(['tag:server', 'tag:tailwatch-hub'])).toBe('tag:tailwatch-hub')
    expect(hubTagFor(['tag:server'])).toBe('tag:server')
    expect(hubTagFor([])).toBeUndefined()
    expect(hubTagFor(undefined)).toBeUndefined()
  })
  it('hides admin-only sections from viewers', () => {
    expect(visibleSections(true).map((s) => s.id)).toEqual(SETTINGS_SECTIONS.map((s) => s.id))
    expect(visibleSections(false).some((s) => s.id === 'audit')).toBe(false)
  })
})

describe('notifiers & audit', () => {
  it('describes known notifiers and passes unknown through', () => {
    expect(notifierMeta('webhook').label).toBe('Webhook')
    expect(notifierMeta('NTFY').kind).toBe('ntfy')
    expect(notifierMeta('pager').label).toBe('pager')
  })
  it('labels audit actions', () => {
    expect(auditActionLabel('device.tags')).toBe('Set tags')
    expect(auditActionLabel('alert.ack')).toBe('Acknowledge alert')
    expect(auditActionLabel('some.new_thing')).toBe('Some new thing')
  })
  it('links audit targets', () => {
    expect(auditTargetLink({ action: 'device.routes', target: 'n1/x' })).toBe('/devices/n1%2Fx')
    expect(auditTargetLink({ action: 'rule.save', target: 'high_cpu' })).toBe('/alerts?tab=rules&rule=high_cpu')
    expect(auditTargetLink({ action: 'alerts.test', target: 'webhook' })).toBeUndefined()
    expect(auditTargetLink({ action: 'device.ping', target: '' })).toBeUndefined()
  })
  it('formats details', () => {
    expect(formatDetails(undefined)).toBe('')
    expect(formatDetails({})).toBe('')
    expect(formatDetails({ a: 1 })).toBe('{\n  "a": 1\n}')
  })
})

describe('agent install snippets', () => {
  it('derives flags from the hub configuration', () => {
    expect(agentFlags({ port: 41820, tokenConfigured: false })).toEqual([])
    expect(agentFlags({ port: 41820, tokenConfigured: true, hubTag: 'tag:tailwatch-hub' })).toEqual(['--allow-tag tag:tailwatch-hub', '--auth both'])
    expect(agentFlags({ port: 5000, tokenConfigured: false, hubName: 'hub' })).toEqual(['--allow-node hub', '--port 5000'])
    // tag wins over node name
    expect(agentFlags({ port: 41820, tokenConfigured: false, hubTag: 'tag:x', hubName: 'hub' })).toEqual(['--allow-tag tag:x'])
  })
  it('produces every platform with commands that reference the deploy scripts', () => {
    const snippets = agentInstallSnippets({ port: 41820, tokenConfigured: true, hubTag: 'tag:tailwatch-hub' })
    expect(snippets.map((s) => s.id)).toEqual(['linux', 'macos', 'windows', 'docker'])
    const linux = snippets[0]!
    expect(linux.steps[0]!.code).toContain('scripts/install-agent.sh')
    expect(linux.steps[0]!.code).toContain('--allow-tag tag:tailwatch-hub --auth both')
    expect(linux.steps[0]!.code).toContain('TAILWATCH_AGENT_TOKEN=')
    expect(linux.steps[1]!.code).toContain('deploy/systemd/tailwatch-agent.service')
    expect(linux.steps[1]!.code).toContain('TAILWATCH_AGENT_TOKEN=<same token as the hub>')
    const mac = snippets[1]!
    expect(mac.steps[1]!.code).toContain('<string>--allow-tag</string>')
    expect(mac.steps[1]!.code).toContain('TAILWATCH_AGENT_TOKEN')
    const win = snippets[2]!
    expect(win.steps[1]!.code).toContain('sc.exe create tailwatch-agent')
    const docker = snippets[3]!
    expect(docker.steps[0]!.code).toContain('command: ["--allow-tag", "tag:tailwatch-hub", "--auth", "both"]')
  })
  it('omits token lines when no token is configured', () => {
    const snippets = agentInstallSnippets({ port: 41820, tokenConfigured: false })
    for (const s of snippets) for (const step of s.steps) expect(step.code).not.toContain('TAILWATCH_AGENT_TOKEN')
    expect(snippets[3]!.steps[0]!.code).not.toContain('command:')
  })
  it('never embeds a token value, only placeholders', () => {
    const snippets = agentInstallSnippets({ port: 41820, tokenConfigured: true })
    const all = snippets.flatMap((s) => s.steps.map((st) => st.code)).join('\n')
    expect(all).not.toMatch(/TAILWATCH_AGENT_TOKEN=[A-Za-z0-9]{16,}/)
  })
})
