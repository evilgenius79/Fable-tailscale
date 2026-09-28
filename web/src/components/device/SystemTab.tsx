import { useMemo } from 'react'
import { Activity, Cpu, HardDrive, MemoryStick, Network, Server, Thermometer } from 'lucide-react'
import type { Device } from '../../api/types'
import { cn } from '../../lib/cn'
import { formatBitrate, formatBytes, formatCompact, formatDateTime, formatDuration, formatInt, formatLoad, formatPercent, formatRelative, formatTemp, plural } from '../../lib/format'
import { agentLabel, agentTone, utilizationTone } from '../../lib/status'
import { Badge, TONE_TEXT } from '../ui/Badge'
import { Card, CardHeader } from '../ui/Card'
import { KeyValueList, type KeyValueItem } from '../ui/KeyValue'
import { Meter } from '../ui/ProgressBar'
import { Table, TBody, TD, TH, THead, TR } from '../ui/Table'
import { AgentEmptyState } from './AgentEmptyState'
import { hasMetrics, interfaceRows, loadPercent } from './deviceDetail'

export interface SystemTabProps {
  device: Device
  agentPort: number
  agentEnabled: boolean
}

function AgentCard({ device }: { device: Device }) {
  const a = device.agent
  const items: KeyValueItem[] = [
    {
      key: 'State',
      value: (
        <Badge tone={agentTone(a.state)} dot>
          {agentLabel(a.state)}
        </Badge>
      ),
    },
    { key: 'Agent version', value: a.version ?? '—', mono: true },
    { key: 'Endpoint', value: a.url ?? '—', mono: true, copy: a.url },
    { key: 'Last success', value: a.lastSuccess ? formatRelative(a.lastSuccess) : '—', hint: a.lastSuccess ? formatDateTime(a.lastSuccess) : undefined },
    { key: 'Last error', value: a.lastError ? <span className="text-critical">{a.lastError}</span> : '—' },
  ]
  return (
    <Card>
      <CardHeader title="Agent" description="tailwatch-agent status as seen by the hub" icon={Activity} />
      <KeyValueList items={items} dense />
    </Card>
  )
}

/** Host, CPU, memory, disks, interfaces, sensors and agent status. */
export function SystemTab({ device, agentPort, agentEnabled }: SystemTabProps) {
  const m = hasMetrics(device) ? device.metrics : undefined
  const ifaces = useMemo(() => (m ? interfaceRows(m) : []), [m])

  if (!m) {
    return (
      <div className="grid gap-4 lg:grid-cols-2 xl:gap-6">
        <AgentEmptyState device={device} agentPort={agentPort} agentEnabled={agentEnabled} size="lg" className="lg:col-span-2" />
        <AgentCard device={device} />
      </div>
    )
  }

  const hostItems: KeyValueItem[] = [
    { key: 'Platform', value: [m.platform, m.platformVersion].filter(Boolean).join(' ') || '—' },
    { key: 'Kernel', value: m.kernel ?? '—', mono: true },
    { key: 'Architecture', value: m.arch ?? '—', mono: true },
    { key: 'Booted', value: m.bootTime ? `${formatDateTime(m.bootTime)} (${formatDuration(m.uptimeSeconds)} ago)` : formatDuration(m.uptimeSeconds) },
    { key: 'Processes', value: formatInt(m.processes) },
    { key: 'Tailscale', value: m.tailscaleVersion ?? device.clientVersion ?? '—', mono: true },
    { key: 'Sampled', value: formatRelative(m.sampledAt), hint: formatDateTime(m.sampledAt) },
  ]
  const loadPct = loadPercent(m.load1, m.cpuCount)
  const cores = m.perCore ?? []
  const swapPct = m.swapTotal > 0 ? (m.swapUsed / m.swapTotal) * 100 : null

  return (
    <div className="grid gap-4 lg:grid-cols-2 xl:gap-6">
      <Card>
        <CardHeader title="Host" description="Platform and kernel reported by the agent" icon={Server} />
        <KeyValueList items={hostItems} dense />
      </Card>

      <Card>
        <CardHeader
          title="CPU"
          description={[m.cpuModel, plural(m.cpuCount, 'core')].filter(Boolean).join(' · ')}
          icon={Cpu}
          actions={<span className={cn('text-sm font-semibold num', TONE_TEXT[utilizationTone(m.cpuPercent)])}>{formatPercent(m.cpuPercent)}</span>}
        />
        <Meter value={m.cpuPercent} label="Total CPU usage" size="md" />
        {cores.length ? (
          <ul className="mt-4 grid grid-cols-2 gap-x-4 gap-y-1.5 sm:grid-cols-4 2xl:grid-cols-8" aria-label="Per-core usage">
            {cores.map((v, i) => (
              <li key={i} className="flex items-center gap-2">
                <span className="w-6 shrink-0 font-mono text-[11px] text-fg-muted num">c{i}</span>
                <Meter value={v} label={`Core ${i} usage`} size="xs" showValue className="min-w-0 flex-1" />
              </li>
            ))}
          </ul>
        ) : null}
        <dl className="mt-4 grid grid-cols-3 gap-3 border-t border-border-subtle pt-3 text-xs">
          {[
            ['Load 1m', m.load1],
            ['Load 5m', m.load5],
            ['Load 15m', m.load15],
          ].map(([label, v]) => (
            <div key={label as string}>
              <dt className="text-fg-muted">{label}</dt>
              <dd className={cn('text-sm font-semibold num', label === 'Load 1m' && loadPct !== null ? TONE_TEXT[utilizationTone(loadPct, 100, 200)] : 'text-fg')}>{formatLoad(v as number)}</dd>
            </div>
          ))}
        </dl>
      </Card>

      <Card>
        <CardHeader
          title="Memory"
          description={`${formatBytes(m.memUsed)} used of ${formatBytes(m.memTotal)} · ${formatBytes(m.memAvailable)} available`}
          icon={MemoryStick}
          actions={<span className={cn('text-sm font-semibold num', TONE_TEXT[utilizationTone(m.memPercent)])}>{formatPercent(m.memPercent)}</span>}
        />
        <Meter value={m.memPercent} label="Memory usage" size="md" />
        <div className="mt-4 flex items-center justify-between text-xs text-fg-muted">
          <span>Swap</span>
          <span className="num">{m.swapTotal > 0 ? `${formatBytes(m.swapUsed)} of ${formatBytes(m.swapTotal)}` : 'none'}</span>
        </div>
        {swapPct !== null ? <Meter value={swapPct} label="Swap usage" size="sm" className="mt-1.5" /> : null}
      </Card>

      <AgentCard device={device} />

      {m.temperatures?.length ? (
        <Card className="lg:col-span-2">
          <CardHeader title="Sensors" description="Temperatures reported by the host" icon={Thermometer} />
          <ul className="grid gap-x-8 gap-y-3 lg:grid-cols-2">
            {m.temperatures.map((t) => {
              const crit = t.critical ?? 100
              return (
                <li key={t.sensor} className="flex items-center gap-3">
                  <span className="w-28 shrink-0 truncate font-mono text-xs text-fg-secondary" title={t.sensor}>
                    {t.sensor}
                  </span>
                  <Meter value={t.celsius} max={crit} thresholds={{ warning: 80, critical: 95 }} label={`${t.sensor} temperature`} size="sm" className="min-w-0 flex-1" />
                  <span className="w-14 shrink-0 text-right text-sm font-semibold num text-fg">{formatTemp(t.celsius)}</span>
                  {t.critical ? <span className="hidden w-16 shrink-0 text-right text-xs text-fg-muted num sm:inline">crit {formatTemp(t.critical)}</span> : null}
                </li>
              )
            })}
          </ul>
        </Card>
      ) : null}


      <Card padding="none" className="lg:col-span-2">
        <CardHeader title="Disks" description={plural(m.disks.length, 'filesystem')} icon={HardDrive} divider />
        <Table bordered={false} dense stickyHeader={false}>
          <THead>
            <TR>
              <TH>Mount</TH>
              <TH hideBelow="sm">Type</TH>
              <TH align="right">Used</TH>
              <TH align="right" hideBelow="md">
                Total
              </TH>
              <TH width="30%">Usage</TH>
            </TR>
          </THead>
          <TBody>
            {m.disks.map((d) => (
              <TR key={d.mount}>
                <TD mono>{d.mount}</TD>
                <TD hideBelow="sm" muted>
                  {d.fstype ?? '—'}
                </TD>
                <TD numeric>{formatBytes(d.used)}</TD>
                <TD numeric hideBelow="md" muted>
                  {formatBytes(d.total)}
                </TD>
                <TD>
                  <Meter value={d.percent} thresholds={{ warning: 80, critical: 95 }} label={`${d.mount} usage`} showValue size="sm" />
                </TD>
              </TR>
            ))}
          </TBody>
        </Table>
      </Card>

      <Card padding="none" className="min-w-0 overflow-hidden lg:col-span-2">
        <CardHeader title="Network interfaces" description="Live rates and lifetime counters" icon={Network} divider />
        <Table bordered={false} dense stickyHeader={false} minWidth={480}>
          <THead>
            <TR>
              <TH>Interface</TH>
              <TH align="right">↓ Rate</TH>
              <TH align="right">↑ Rate</TH>
              <TH align="right" hideBelow="md">
                ↓ Total
              </TH>
              <TH align="right" hideBelow="md">
                ↑ Total
              </TH>
              <TH align="right" hideBelow="lg">
                Packets
              </TH>
              <TH align="right">Errors</TH>
            </TR>
          </THead>
          <TBody>
            {ifaces.map((i) => {
              const errors = i.rxErrors + i.txErrors
              return (
                <TR key={i.name}>
                  <TD>
                    <span className="inline-flex items-center gap-2">
                      <span className="font-mono text-xs text-fg">{i.name}</span>
                      {i.tailscale ? (
                        <Badge size="sm" tone="accent">
                          tailscale
                        </Badge>
                      ) : null}
                    </span>
                  </TD>
                  <TD numeric>{formatBitrate(i.rxRate)}</TD>
                  <TD numeric>{formatBitrate(i.txRate)}</TD>
                  <TD numeric hideBelow="md" muted>
                    {formatBytes(i.rxBytes)}
                  </TD>
                  <TD numeric hideBelow="md" muted>
                    {formatBytes(i.txBytes)}
                  </TD>
                  <TD numeric hideBelow="lg" muted>
                    {formatCompact(i.rxPackets + i.txPackets)}
                  </TD>
                  <TD numeric>
                    {errors > 0 ? (
                      <Badge size="sm" tone="warning">
                        {formatInt(errors)}
                      </Badge>
                    ) : (
                      <span className="text-fg-muted">0</span>
                    )}
                  </TD>
                </TR>
              )
            })}
          </TBody>
        </Table>
      </Card>

    </div>
  )
}
