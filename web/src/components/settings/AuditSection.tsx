import { Fragment, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { ChevronDown, ScrollText } from 'lucide-react'
import type { AuditEntry } from '../../api/types'
import { cn } from '../../lib/cn'
import { formatDateTime, formatInt, formatRelative, plural } from '../../lib/format'
import { Badge } from '../ui/Badge'
import { EmptyState } from '../ui/EmptyState'
import { ErrorState } from '../ui/ErrorState'
import { SearchInput } from '../ui/Input'
import { SkeletonTable } from '../ui/Skeleton'
import { TBody, TD, TH, THead, TR, Table, TableMessage } from '../ui/Table'
import { Tooltip } from '../ui/Tooltip'
import { SettingsSection } from './SettingsSection'
import { auditActionLabel, auditTargetLink, formatDetails } from './settings'

export interface AuditSectionProps {
  entries: AuditEntry[] | undefined
  loading: boolean
  error: unknown
  onRetry: () => void
  deviceName?: (id: string) => string | undefined
}

const COLS = 6

export function AuditSection({ entries, loading, error, onRetry, deviceName }: AuditSectionProps) {
  const [q, setQ] = useState('')
  const [open, setOpen] = useState<Set<number>>(() => new Set())
  const rows = useMemo(() => {
    const s = q.trim().toLowerCase()
    const list = entries ?? []
    if (!s) return list
    return list.filter((e) => `${e.actor} ${e.actorNode} ${e.action} ${auditActionLabel(e.action)} ${e.target} ${e.error ?? ''} ${deviceName?.(e.target) ?? ''}`.toLowerCase().includes(s))
  }, [entries, q, deviceName])
  const toggle = (id: number) =>
    setOpen((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  const now = Date.now()

  return (
    <SettingsSection
      id="audit"
      icon={ScrollText}
      title="Audit log"
      description="Every admin action, successful or not, with who ran it and from which node. Admin-only."
      flush
      toolbar={<SearchInput size="sm" value={q} onValueChange={setQ} placeholder="Filter actor, action, target…" aria-label="Filter audit log" className="w-full sm:w-72" />}
    >
      {error ? (
        <div className="p-4">
          <ErrorState compact error={error} onRetry={onRetry} />
        </div>
      ) : loading ? (
        <div className="p-4">
          <SkeletonTable rows={5} cols={5} />
        </div>
      ) : !entries?.length ? (
        <EmptyState size="sm" bordered={false} icon={ScrollText} title="No admin actions yet" description="Acknowledgements, rule changes and device actions will be recorded here." />
      ) : (
        <>
          <Table bordered={false} minWidth={640} maxHeight={480} dense>
            <THead>
              <TR>
                <TH width={120}>When</TH>
                <TH>Actor</TH>
                <TH width={160}>Action</TH>
                <TH>Target</TH>
                <TH width={90}>Result</TH>
                <TH width={40} aria-label="Details" />
              </TR>
            </THead>
            <TBody>
              {rows.length === 0 ? (
                <TableMessage colSpan={COLS}>No entries match “{q}”.</TableMessage>
              ) : (
                rows.map((e) => {
                  const expanded = open.has(e.id)
                  const details = formatDetails(e.details)
                  const link = auditTargetLink(e)
                  const targetLabel = e.action.startsWith('device.') ? deviceName?.(e.target) ?? e.target : e.target
                  const hasMore = !!details || !!e.error || !!e.remoteIp
                  return (
                    <Fragment key={e.id}>
                      <TR onClick={hasMore ? () => toggle(e.id) : undefined} aria-expanded={hasMore ? expanded : undefined} className={cn(!e.ok && 'bg-critical-soft/30')}>
                        <TD muted className="whitespace-nowrap">
                          <Tooltip content={formatDateTime(e.ts)}>
                            <time dateTime={e.ts} tabIndex={0} className="num rounded focus-ring">
                              {formatRelative(e.ts, now)}
                            </time>
                          </Tooltip>
                        </TD>
                        <TD>
                          <span className="block truncate text-[13px] text-fg">{e.actor}</span>
                          <span className="block truncate font-mono text-[11px] text-fg-muted">{e.actorNode}</span>
                        </TD>
                        <TD>
                          <span className="block truncate font-medium text-fg">{auditActionLabel(e.action)}</span>
                          <span className="block truncate font-mono text-[11px] text-fg-muted">{e.action}</span>
                        </TD>
                        <TD truncate>
                          {link ? (
                            <Link to={link} className="rounded font-medium text-fg-secondary hover:text-accent-text focus-ring" onClick={(ev) => ev.stopPropagation()}>
                              {targetLabel}
                            </Link>
                          ) : (
                            <span className="text-fg-secondary">{targetLabel || '—'}</span>
                          )}
                        </TD>
                        <TD>
                          <Badge size="sm" tone={e.ok ? 'online' : 'critical'} dot>
                            {e.ok ? 'ok' : 'failed'}
                          </Badge>
                        </TD>
                        <TD align="center">
                          {hasMore ? (
                            <ChevronDown className={cn('size-4 text-fg-muted transition-transform', expanded && 'rotate-180')} aria-hidden="true" />
                          ) : null}
                        </TD>
                      </TR>
                      {expanded ? (
                        <tr className="bg-surface-inset/60">
                          <td colSpan={COLS} className="border-b border-border-subtle px-4 py-3">
                            <div className="grid gap-3 text-xs sm:grid-cols-[auto_minmax(0,1fr)] sm:gap-x-6">
                              {e.error ? (
                                <>
                                  <span className="font-semibold uppercase tracking-wider text-fg-muted">Error</span>
                                  <p className="break-words font-mono text-critical">{e.error}</p>
                                </>
                              ) : null}
                              {e.remoteIp ? (
                                <>
                                  <span className="font-semibold uppercase tracking-wider text-fg-muted">From</span>
                                  <p className="font-mono text-fg-secondary">{e.remoteIp}</p>
                                </>
                              ) : null}
                              <span className="font-semibold uppercase tracking-wider text-fg-muted">Entry</span>
                              <p className="font-mono text-fg-secondary">
                                #{e.id} · {formatDateTime(e.ts)}
                              </p>
                              {details ? (
                                <>
                                  <span className="font-semibold uppercase tracking-wider text-fg-muted">Details</span>
                                  <pre className="overflow-x-auto rounded-md border border-border bg-surface p-2 font-mono text-[11px] leading-4 text-fg scrollbar-thin">{details}</pre>
                                </>
                              ) : null}
                            </div>
                          </td>
                        </tr>
                      ) : null}
                    </Fragment>
                  )
                })
              )}
            </TBody>
          </Table>
          <p className="border-t border-border px-4 py-2 text-xs text-fg-muted">
            {q ? `${formatInt(rows.length)} of ${plural(entries.length, 'entry', 'entries')}` : `Latest ${plural(entries.length, 'entry', 'entries')}`}
          </p>
        </>
      )}
    </SettingsSection>
  )
}
