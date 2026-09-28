import { Activity, PlugZap, Smartphone, WifiOff } from 'lucide-react'
import type { Device } from '../../api/types'
import { cn } from '../../lib/cn'
import { formatRelative } from '../../lib/format'
import { osFamily } from '../../lib/os'
import { CopyButton } from '../ui/CopyButton'
import { EmptyState } from '../ui/EmptyState'
import { agentInstallCommand } from './deviceDetail'

export interface AgentEmptyStateProps {
  device: Device
  /** Hub-side agent port (settings.agentPort). */
  agentPort: number
  /** settings.agentEnabled — when false the hub never polls agents. */
  agentEnabled?: boolean
  size?: 'sm' | 'md' | 'lg'
  className?: string
}

/**
 * Friendly explanation of why there are no system metrics for a device, with
 * the install command when an agent would help.
 */
export function AgentEmptyState({ device, agentPort, agentEnabled = true, size = 'md', className }: AgentEmptyStateProps) {
  const fam = osFamily(device.os)
  const mobile = fam === 'ios' || fam === 'android' || fam === 'tvos'
  const state = device.agent.state

  if (!device.online) {
    return (
      <EmptyState
        icon={WifiOff}
        size={size}
        className={className}
        title="Device is offline"
        description={`System metrics resume when the device reconnects. Last seen ${formatRelative(device.lastSeen)}.`}
      />
    )
  }
  if (mobile) {
    return (
      <EmptyState
        icon={Smartphone}
        size={size}
        className={className}
        title="No system metrics on mobile devices"
        description="The tailwatch agent runs on Linux, macOS, Windows and BSD hosts. Connectivity, uptime and traffic are still tracked for this device."
      />
    )
  }
  if (!agentEnabled || state === 'disabled') {
    return (
      <EmptyState
        icon={PlugZap}
        size={size}
        className={className}
        title="Agent polling is disabled"
        description="The hub was started without agent collection. Enable it with --agent-enabled to gather system metrics from devices running the agent."
      />
    )
  }
  if (state === 'unreachable') {
    return (
      <EmptyState
        icon={PlugZap}
        size={size}
        className={className}
        title="Agent unreachable"
        description={
          <>
            The hub could not reach the agent at <span className="font-mono text-xs text-fg">{device.agent.url ?? `port ${agentPort}`}</span>
            {device.agent.lastSuccess ? <> (last success {formatRelative(device.agent.lastSuccess)})</> : null}.{' '}
            {device.agent.lastError ? <span className="text-critical">{device.agent.lastError}</span> : 'Check the service and the ACL rule allowing the hub to reach it.'}
          </>
        }
      />
    )
  }
  const cmd = agentInstallCommand(agentPort)
  return (
    <EmptyState
      icon={Activity}
      size={size}
      className={className}
      title="No agent on this device"
      description="Install tailwatch-agent to collect CPU, memory, disk, network and temperature metrics. Connectivity and uptime are tracked without it."
      action={
        <div className={cn('flex w-full max-w-xl items-start gap-2 rounded-md border border-border bg-surface-inset p-2 text-left')}>
          <pre className="min-w-0 flex-1 overflow-x-auto whitespace-pre-wrap break-all font-mono text-[11px] leading-5 text-fg-secondary scrollbar-thin">{cmd}</pre>
          <CopyButton value={cmd} label="Copy install command" size="xs" />
        </div>
      }
    />
  )
}
