import { useEffect, useState, type ReactNode } from 'react'
import type { LucideIcon } from 'lucide-react'
import { cn } from '../../lib/cn'
import { Card, CardHeader } from '../ui/Card'

export interface SettingsSectionProps {
  id: string
  title: string
  description?: ReactNode
  icon?: LucideIcon
  actions?: ReactNode
  /** Full-width row under the title (filters, search) — wraps cleanly on phones. */
  toolbar?: ReactNode
  /** Remove body padding (tables, lists). */
  flush?: boolean
  children: ReactNode
  className?: string
}

/** One anchored settings card: header with icon/title/description/actions, padded (or flush) body. */
export function SettingsSection({ id, title, description, icon, actions, toolbar, flush, children, className }: SettingsSectionProps) {
  return (
    <Card as="section" padding="none" id={id} aria-labelledby={`${id}-title`} className={cn('scroll-mt-[calc(var(--topbar-h)+16px)]', className)}>
      <CardHeader divider icon={icon} title={<span id={`${id}-title`}>{title}</span>} description={description} actions={actions}>
        {toolbar}
      </CardHeader>
      <div className={cn(!flush && 'p-4 sm:p-5')}>{children}</div>
    </Card>
  )
}

export interface SettingsNavProps {
  sections: ReadonlyArray<{ id: string; label: string }>
  className?: string
}

/** Sticky in-page navigation (vertical on lg+, a scrollable chip row below) with a scroll-spy highlight. */
export function SettingsNav({ sections, className }: SettingsNavProps) {
  const [active, setActive] = useState<string>(sections[0]?.id ?? '')

  useEffect(() => {
    if (typeof IntersectionObserver === 'undefined') return
    const els = sections.map((s) => document.getElementById(s.id)).filter((el): el is HTMLElement => !!el)
    if (!els.length) return
    const visible = new Map<string, number>()
    const io = new IntersectionObserver(
      (entries) => {
        for (const e of entries) {
          if (e.isIntersecting) visible.set(e.target.id, e.boundingClientRect.top)
          else visible.delete(e.target.id)
        }
        if (!visible.size) return
        // The topmost visible section wins.
        const top = Array.from(visible.entries()).sort((a, b) => a[1] - b[1])[0]
        if (top) setActive(top[0])
      },
      { rootMargin: '-72px 0px -60% 0px', threshold: [0, 0.2] },
    )
    for (const el of els) io.observe(el)
    return () => io.disconnect()
  }, [sections])

  const onClick = (id: string) => (e: React.MouseEvent) => {
    const el = document.getElementById(id)
    if (!el) return
    e.preventDefault()
    el.scrollIntoView({ behavior: 'smooth', block: 'start' })
    setActive(id)
    history.replaceState(null, '', `#${id}`)
  }

  return (
    <nav aria-label="Settings sections" className={cn('lg:sticky lg:top-[calc(var(--topbar-h)+24px)] lg:self-start', className)}>
      <ul className="-mx-4 flex gap-1 overflow-x-auto px-4 pb-1 scrollbar-none lg:mx-0 lg:flex-col lg:overflow-visible lg:px-0 lg:pb-0">
        {sections.map((s) => {
          const on = s.id === active
          return (
            <li key={s.id} className="shrink-0">
              <a
                href={`#${s.id}`}
                onClick={onClick(s.id)}
                aria-current={on ? 'location' : undefined}
                className={cn(
                  'relative flex h-8 items-center whitespace-nowrap rounded-md px-2.5 text-[13px] font-medium transition-colors focus-ring',
                  on ? 'bg-surface-active text-fg' : 'text-fg-secondary hover:bg-surface-hover hover:text-fg',
                )}
              >
                {on ? <span aria-hidden="true" className="absolute -left-2 top-1/2 hidden h-4 w-0.5 -translate-y-1/2 rounded-full bg-accent lg:block" /> : null}
                {s.label}
              </a>
            </li>
          )
        })}
      </ul>
    </nav>
  )
}
