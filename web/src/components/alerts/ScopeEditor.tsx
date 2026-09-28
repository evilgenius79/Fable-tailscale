import { useState, type KeyboardEvent } from 'react'
import { Plus, X } from 'lucide-react'
import type { Device } from '../../api/types'
import { cn } from '../../lib/cn'
import { Button } from '../ui/Button'
import { Input } from '../ui/Input'
import { Select } from '../ui/Select'
import { Tag } from '../ui/Tag'
import { normalizeTag, validateTag } from './alerts'

// ---------------------------------------------------------------------------
// Tags
// ---------------------------------------------------------------------------

export interface TagListEditorProps {
  label: string
  hint?: string
  tags: string[]
  onChange: (tags: string[]) => void
  /** Tags seen on the fleet, offered as suggestions. */
  suggestions?: string[]
  disabled?: boolean
  idPrefix: string
}

/** Chip list + validated input for ACL tags ("tag:server"). Enter or comma adds. */
export function TagListEditor({ label, hint, tags, onChange, suggestions = [], disabled, idPrefix }: TagListEditorProps) {
  const [draft, setDraft] = useState('')
  const [error, setError] = useState<string | null>(null)
  const inputId = `${idPrefix}-input`
  const errId = `${idPrefix}-err`
  const listId = `${idPrefix}-list`

  const add = (raw: string) => {
    const tag = normalizeTag(raw)
    const err = validateTag(tag, tags)
    if (err) {
      setError(err)
      return false
    }
    onChange([...tags, tag])
    setDraft('')
    setError(null)
    return true
  }
  const onKey = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter' || e.key === ',') {
      e.preventDefault()
      if (draft.trim()) add(draft)
    } else if (e.key === 'Backspace' && !draft && tags.length) {
      onChange(tags.slice(0, -1))
    }
  }
  const remaining = suggestions.filter((s) => !tags.includes(s))

  return (
    <div className="space-y-1.5">
      <label htmlFor={inputId} className="text-[13px] font-medium text-fg">
        {label}
      </label>
      {tags.length ? (
        <ul className="flex flex-wrap gap-1" aria-label={`${label} list`} id={listId}>
          {tags.map((t) => (
            <li key={t}>
              <Tag tag={t} onRemove={disabled ? undefined : () => onChange(tags.filter((x) => x !== t))} />
            </li>
          ))}
        </ul>
      ) : null}
      {!disabled ? (
        <div className="flex items-center gap-2">
          <Input
            id={inputId}
            size="sm"
            mono
            value={draft}
            placeholder="tag:name"
            list={remaining.length ? `${idPrefix}-suggest` : undefined}
            invalid={!!error}
            aria-describedby={error ? errId : undefined}
            onChange={(e) => {
              setDraft(e.target.value)
              if (error) setError(null)
            }}
            onKeyDown={onKey}
            onBlur={() => {
              if (draft.trim()) add(draft)
            }}
            className="w-full max-w-[240px]"
          />
          {remaining.length ? (
            <datalist id={`${idPrefix}-suggest`}>
              {remaining.map((s) => (
                <option key={s} value={s} />
              ))}
            </datalist>
          ) : null}
          <Button size="sm" variant="ghost" leadingIcon={Plus} onClick={() => draft.trim() && add(draft)} disabled={!draft.trim()}>
            Add
          </Button>
        </div>
      ) : !tags.length ? (
        <p className="text-xs text-fg-faint">None</p>
      ) : null}
      {error ? (
        <p id={errId} role="alert" className="text-xs text-critical">
          {error}
        </p>
      ) : hint ? (
        <p className="text-xs text-fg-muted">{hint}</p>
      ) : null}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Devices
// ---------------------------------------------------------------------------

export interface DeviceListEditorProps {
  label: string
  hint?: string
  ids: string[]
  onChange: (ids: string[]) => void
  devices: ReadonlyArray<Pick<Device, 'id' | 'name'>>
  disabled?: boolean
  idPrefix: string
}

/** Chip list + select for device ids; unknown ids (devices since removed) still render by id. */
export function DeviceListEditor({ label, hint, ids, onChange, devices, disabled, idPrefix }: DeviceListEditorProps) {
  const selectId = `${idPrefix}-select`
  const nameOf = (id: string) => devices.find((d) => d.id === id)?.name ?? id
  const remaining = devices.filter((d) => !ids.includes(d.id)).sort((a, b) => a.name.localeCompare(b.name))
  return (
    <div className="space-y-1.5">
      <label htmlFor={selectId} className="text-[13px] font-medium text-fg">
        {label}
      </label>
      {ids.length ? (
        <ul className="flex flex-wrap gap-1" aria-label={`${label} list`}>
          {ids.map((id) => (
            <li key={id} className={cn('inline-flex h-6 max-w-full items-center gap-0.5 rounded-md border border-border bg-surface-inset pl-2 text-xs text-fg', disabled ? 'pr-2' : 'pr-0.5')}>
              <span className="truncate">{nameOf(id)}</span>
              {!disabled ? (
                <button type="button" onClick={() => onChange(ids.filter((x) => x !== id))} aria-label={`Remove ${nameOf(id)}`} className="rounded p-0.5 text-fg-muted hover:bg-surface-hover hover:text-fg focus-ring">
                  <X className="size-3" aria-hidden="true" />
                </button>
              ) : null}
            </li>
          ))}
        </ul>
      ) : disabled ? (
        <p className="text-xs text-fg-faint">None</p>
      ) : null}
      {!disabled ? (
        <Select
          id={selectId}
          size="sm"
          value=""
          placeholder={remaining.length ? 'Add a device…' : 'No more devices'}
          disabled={!remaining.length}
          onValueChange={(v) => v && onChange([...ids, v])}
          options={remaining.map((d) => ({ value: d.id, label: d.name }))}
          className="w-full max-w-[240px]"
        />
      ) : null}
      {hint ? <p className="text-xs text-fg-muted">{hint}</p> : null}
    </div>
  )
}
