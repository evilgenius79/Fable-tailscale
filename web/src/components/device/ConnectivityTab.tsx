import { useMemo } from 'react'
import { ArrowDown, ArrowUp, Check, KeyRound, Minus, Network, Radio, Route, ShieldAlert, Waypoints, X } from 'lucide-react'
import { useTopology } from '../../api/hooks'
import type { Device } from '../../api/types'
import { cn } from '../../lib/cn'
import { daysUntil, formatBitrate, formatBytes, formatDateTime, formatLatency, formatRelative, plural, shortVersion } from '../../lib/format'
import { pathLabel, pathTone } from '../../lib/status'
import { Badge } from '../ui/Badge'
import { Button } from '../ui/Button'
import { Card, CardHeader } from '../ui/Card'
import { CopyButton } from '../ui/CopyButton'
import { EmptyState } from '../ui/EmptyState'
import { KeyValueList, type KeyValueItem } from '../ui/KeyValue'
import { Meter } from '../ui/ProgressBar'
import { Table, TBody, TD, TH, THead, TR } from '../ui/Table'
import { derpRows, natRows, routeRows } from './deviceDetail'

export interface ConnectivityTabProps {
  device: Device
  canManage: boolean
  onApproveRoutes?: () => void
}

function keyExpiryValue(d: Device): { text: string; tone: 'critical' | 'warning' | 'neutral' | 'online' } {
  if (d.expired) return { text: 'Expired', tone: 'critical' }
  if (d.keyExpiryDisabled) return { text: 'Disabled (never expires)', tone: 'neutral' }
  if (!d.keyExpiry) return { text: '—', tone: 'neutral' }
  const days = daysUntil(d.keyExpiry)
  const rel = formatRelative(d.keyExpiry)
  if (days !== null && days < 2) return { text: `${formatDateTime(d.keyExpiry)} (${rel})`, tone: 'critical' }
  if (days !== null && days < 7) return { text: `${formatDateTime(d.keyExpiry)} (${rel})`, tone: 'warning' }
  return { text: `${formatDateTime(d.keyExpiry)} (${rel})`, tone: 'online' }
}

const TONE_TEXT_CLS = { critical: 'text-critical', warning: 'text-warning', neutral: 'text-fg', online: 'text-fg' } as const

/** Path, endpoints, DERP latency, NAT matrix, node key and routes. */
export function ConnectivityTab({ device, canManage, onApproveRoutes }: ConnectivityTabProps) {
  const topo = useTopology()
  const regions = topo.data?.derpRegions ?? {}
  const c = device.connectivity
  const derp = useMemo(() => derpRows(c.derpLatencyMs, c.preferredDerp, regions), [c.derpLatencyMs, c.preferredDerp, regions])
  const nat = useMemo(() => natRows(c.natSupport, c.mappingVariesByDestIp), [c.natSupport, c.mappingVariesByDestIp])
  const routes = useMemo(() => routeRows(device), [device])
  const maxDerp = derp.length ? Math.max(...derp.map((r) => r.latencyMs)) : 0
  const key = keyExpiryValue(device)

  const pathItems: KeyValueItem[] = [
    {
      key: 'Path',
      value: (
        <Badge tone={device.online ? pathTone(c.path) : 'offline'} dot>
          {device.online ? pathLabel(c.path) : 'Offline'}
        </Badge>
      ),
    },
    ...(c.path === 'relay' && c.relay
      ? [{ key: 'DERP relay', value: `${c.relay}${regions[c.relay] ? ` · ${regions[c.relay]}` : ''}` }]
      : c.preferredDerp
        ? [{ key: 'Home DERP', value: `${c.preferredDerp}${regions[c.preferredDerp] ? ` · ${regions[c.preferredDerp]}` : ''}`, hint: 'Preferred relay region when a relay is needed' }]
        : []),
    { key: 'Current endpoint', value: c.curAddr ?? '—', mono: true, copy: c.curAddr },
    { key: 'Latency', value: device.online ? formatLatency(c.latencyMs) : '—', hint: c.lastPing ? `Last ping ${formatDateTime(c.lastPing)}` : undefined },
    { key: 'Last ping', value: c.lastPing ? formatRelative(c.lastPing) : '—', hint: c.lastPing ? formatDateTime(c.lastPing) : undefined },
    { key: 'Last handshake', value: device.lastHandshake ? formatRelative(device.lastHandshake) : '—', hint: device.lastHandshake ? formatDateTime(device.lastHandshake) : undefined },
    {
      key: 'Throughput now',
      value: (
        <span className="inline-flex items-center gap-2 num">
          <span className="inline-flex items-center gap-0.5">
            <ArrowDown className="size-3 text-fg-muted" aria-hidden="true" />
            <span className="sr-only">Download </span>
            {formatBitrate(c.rxRate)}
          </span>
          <span className="inline-flex items-center gap-0.5">
            <ArrowUp className="size-3 text-fg-muted" aria-hidden="true" />
            <span className="sr-only">Upload </span>
            {formatBitrate(c.txRate)}
          </span>
        </span>
      ),
    },
    {
      key: 'Total transferred',
      value: (
        <span className="inline-flex items-center gap-2 num">
          <span className="inline-flex items-center gap-0.5">
            <ArrowDown className="size-3 text-fg-muted" aria-hidden="true" />
            <span className="sr-only">Received </span>
            {formatBytes(c.rxBytes)}
          </span>
          <span className="inline-flex items-center gap-0.5">
            <ArrowUp className="size-3 text-fg-muted" aria-hidden="true" />
            <span className="sr-only">Sent </span>
            {formatBytes(c.txBytes)}
          </span>
        </span>
      ),
      hint: 'Since the hub started tracking this peer',
    },
  ]

  const keyItems: KeyValueItem[] = [
    { key: 'Key expiry', value: <span className={TONE_TEXT_CLS[key.tone]}>{key.text}</span>, wrap: true },
    {
      key: 'Client version',
      value: (
        <span className="inline-flex items-center gap-1.5">
          <span className="num">{shortVersion(device.clientVersion)}</span>
          {device.updateAvailable ? (
            <Badge size="sm" tone="info">
              update available
            </Badge>
          ) : null}
        </span>
      ),
      hint: device.clientVersion,
    },
    { key: 'Authorized', value: device.authorized ? 'Yes' : <span className="text-critical">No — waiting for approval</span> },
    { key: 'Incoming connections', value: device.blocksIncomingConnections ? 'Blocked (shields up)' : 'Allowed' },
    { key: 'Created', value: formatDateTime(device.created), hint: formatRelative(device.created) },
    { key: 'First seen', value: formatDateTime(device.firstSeen), hint: formatRelative(device.firstSeen) },
    { key: 'Last seen', value: formatRelative(device.lastSeen), hint: formatDateTime(device.lastSeen) },
    { key: 'Node ID', value: device.id, mono: true, copy: device.id },
  ]

  return (
    <div className="grid gap-4 lg:grid-cols-2 xl:gap-6">
      <Card>
        <CardHeader title="Path" description="How the hub currently reaches this peer" icon={Waypoints} />
        <KeyValueList items={pathItems} dense />
      </Card>

      <Card>
        <CardHeader title="Node key" description="Identity, authorization and client" icon={KeyRound} />
        <KeyValueList items={keyItems} dense />
      </Card>

      <Card>
        <CardHeader
          title="DERP latency"
          description="Round-trip to each relay region, best first"
          icon={Radio}
          actions={c.preferredDerp ? <span className="text-xs text-fg-muted">home: {c.preferredDerp}</span> : null}
        />
        {derp.length ? (
          <Table bordered={false} dense stickyHeader={false} containerClassName="-mx-1">
            <THead>
              <TR>
                <TH>Region</TH>
                <TH align="right">Latency</TH>
                <TH width="40%" hideBelow="sm">
                  <span className="sr-only">Relative latency</span>
                </TH>
              </TR>
            </THead>
            <TBody>
              {derp.map((r) => (
                <TR key={r.code} className={cn(r.preferred && 'bg-accent-soft/40')}>
                  <TD>
                    <span className="inline-flex items-center gap-2">
                      <span className="font-mono text-xs font-semibold text-fg">{r.code}</span>
                      <span className="truncate text-fg-secondary">{r.name !== r.code ? r.name : ''}</span>
                      {r.preferred ? (
                        <Badge size="sm" tone="accent">
                          preferred
                        </Badge>
                      ) : null}
                    </span>
                  </TD>
                  <TD numeric>{formatLatency(r.latencyMs)}</TD>
                  <TD hideBelow="sm">
                    <Meter value={r.latencyMs} max={maxDerp || 1} tone={r.preferred ? 'accent' : 'neutral'} size="xs" label={`${r.code} latency relative to the slowest region`} />
                  </TD>
                </TR>
              ))}
            </TBody>
          </Table>
        ) : (
          <EmptyState size="sm" bordered={false} icon={Radio} title="No DERP probes reported" description="Latency to relay regions appears once the control API reports client connectivity." />
        )}
      </Card>

      <Card>
        <CardHeader title="NAT traversal" description="Capabilities reported by the client" icon={Network} />
        <ul className="grid gap-x-6 sm:grid-cols-2">
          {nat.map((r) => {
            const good = r.supported === null ? null : r.supported === r.goodWhenTrue
            const Icon = r.supported === null ? Minus : r.supported ? Check : X
            return (
              <li key={r.id} className="flex items-start gap-2.5 border-b border-border-subtle py-2 last:border-b-0 sm:[&:nth-last-child(2)]:border-b-0">
                <span
                  className={cn(
                    'mt-0.5 flex size-5 shrink-0 items-center justify-center rounded-full',
                    good === null ? 'bg-surface-inset text-fg-faint' : good ? 'bg-online-soft text-online' : 'bg-warning-soft text-warning',
                  )}
                  aria-hidden="true"
                >
                  <Icon className="size-3" />
                </span>
                <span className="min-w-0">
                  <span className="flex items-center gap-2 text-[13px] text-fg">
                    {r.label}
                    <span className={cn('text-xs', good === null ? 'text-fg-faint' : good ? 'text-online' : 'text-warning')}>
                      {r.supported === null ? 'unknown' : r.supported ? (r.goodWhenTrue ? 'supported' : 'yes') : r.goodWhenTrue ? 'unsupported' : 'no'}
                    </span>
                  </span>
                  <span className="block text-xs text-fg-muted">{r.detail}</span>
                </span>
              </li>
            )
          })}
        </ul>
      </Card>

      <Card>
        <CardHeader title="Candidate endpoints" description="Addresses this device advertises for direct connections" icon={Radio} />
        {c.endpoints?.length ? (
          <ul className="divide-y divide-border-subtle">
            {c.endpoints.map((ep) => (
              <li key={ep} className="group flex items-center justify-between gap-3 py-1.5">
                <span className="inline-flex min-w-0 items-center gap-2">
                  <span className="truncate font-mono text-xs text-fg">{ep}</span>
                  {ep === c.curAddr ? (
                    <Badge size="sm" tone="direct" dot>
                      active
                    </Badge>
                  ) : null}
                </span>
                <CopyButton value={ep} label={`Copy ${ep}`} size="xs" className="opacity-0 group-hover:opacity-100 focus:opacity-100" />
              </li>
            ))}
          </ul>
        ) : (
          <EmptyState size="sm" bordered={false} icon={Radio} title="No endpoints reported" />
        )}
      </Card>

      <Card>
        <CardHeader
          title="Routes"
          description={routes.length ? `${plural(routes.filter((r) => r.enabled).length, 'route')} enabled of ${routes.length} advertised` : 'Subnet routes and exit-node offers'}
          icon={Route}
          actions={
            canManage && routes.length && onApproveRoutes ? (
              <Button size="xs" variant="outline" onClick={onApproveRoutes}>
                Approve routes
              </Button>
            ) : null
          }
        />
        {routes.length ? (
          <Table bordered={false} dense stickyHeader={false} containerClassName="-mx-1">
            <THead>
              <TR>
                <TH>Route</TH>
                <TH>Status</TH>
                <TH hideBelow="sm">Primary</TH>
              </TR>
            </THead>
            <TBody>
              {routes.map((r) => (
                <TR key={r.route}>
                  <TD mono>
                    <span className="inline-flex items-center gap-2">
                      {r.route}
                      {r.exit ? (
                        <Badge size="sm" tone="info">
                          exit node
                        </Badge>
                      ) : null}
                    </span>
                  </TD>
                  <TD>
                    <Badge size="sm" tone={r.enabled ? 'online' : 'warning'} dot>
                      {r.enabled ? 'Enabled' : 'Awaiting approval'}
                    </Badge>
                  </TD>
                  <TD hideBelow="sm" muted>
                    {r.primary ? 'Primary' : '—'}
                  </TD>
                </TR>
              ))}
            </TBody>
          </Table>
        ) : (
          <EmptyState size="sm" bordered={false} icon={ShieldAlert} title="No routes advertised" description="This device does not act as a subnet router or exit node." />
        )}
      </Card>
    </div>
  )
}
