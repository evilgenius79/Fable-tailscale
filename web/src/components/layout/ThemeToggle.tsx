import { Monitor, Moon, Sun } from 'lucide-react'
import { useUIStore } from '../../store'
import type { ThemePreference } from '../../lib/theme'
import { IconButton } from '../ui/Button'
import { DropdownMenu } from '../ui/Menu'

const OPTIONS: { id: ThemePreference; label: string; icon: typeof Sun }[] = [
  { id: 'system', label: 'System', icon: Monitor },
  { id: 'light', label: 'Light', icon: Sun },
  { id: 'dark', label: 'Dark', icon: Moon },
]

/** Theme menu: system / light / dark. Persisted to localStorage (preference only). */
export function ThemeToggle() {
  const theme = useUIStore((s) => s.theme)
  const resolved = useUIStore((s) => s.resolvedTheme)
  const setTheme = useUIStore((s) => s.setTheme)
  const Icon = theme === 'system' ? Monitor : resolved === 'dark' ? Moon : Sun
  const current = OPTIONS.find((o) => o.id === theme)?.label ?? 'System'
  return (
    <DropdownMenu
      label="Theme"
      align="end"
      width={160}
      items={OPTIONS.map((o) => ({ id: o.id, label: o.label, icon: o.icon, checked: theme === o.id, onSelect: () => setTheme(o.id) }))}
      trigger={<IconButton icon={Icon} label={`Theme: ${current}`} size="sm" />}
    />
  )
}
