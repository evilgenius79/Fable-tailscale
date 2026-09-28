import { useId, type ComponentProps, type ReactNode } from 'react'
import type { LucideIcon } from 'lucide-react'
import { Search, X } from 'lucide-react'
import { cn } from '../../lib/cn'

export type InputSize = 'sm' | 'md' | 'lg'

const SIZES: Record<InputSize, string> = { sm: 'h-8 text-[13px]', md: 'h-9 text-sm', lg: 'h-10 text-sm' }

export const inputBase =
  'w-full min-w-0 rounded-md border border-border bg-surface-raised text-fg placeholder:text-fg-faint shadow-xs transition-colors hover:border-border-strong focus:border-accent focus:outline-none focus:ring-2 focus:ring-ring/60 disabled:opacity-50 disabled:cursor-not-allowed aria-[invalid=true]:border-critical-fill aria-[invalid=true]:focus:ring-critical/40'

export interface InputProps extends Omit<ComponentProps<'input'>, 'size'> {
  size?: InputSize
  leadingIcon?: LucideIcon
  /** Right-side slot (button, kbd hint, unit). */
  trailing?: ReactNode
  invalid?: boolean
  /** Monospace value (ids, CIDRs). */
  mono?: boolean
}

export function Input({ size = 'md', leadingIcon: Leading, trailing, invalid, mono, className, ...rest }: InputProps) {
  return (
    <div className={cn('relative flex items-center', className)}>
      {Leading ? <Leading className="pointer-events-none absolute left-2.5 size-4 text-fg-muted" aria-hidden="true" /> : null}
      <input
        aria-invalid={invalid || undefined}
        className={cn(inputBase, SIZES[size], Leading ? 'pl-8' : 'pl-3', trailing ? 'pr-9' : 'pr-3', mono && 'font-mono')}
        {...rest}
      />
      {trailing ? <div className="absolute right-1.5 flex items-center">{trailing}</div> : null}
    </div>
  )
}

export interface SearchInputProps extends Omit<InputProps, 'value' | 'onChange' | 'leadingIcon' | 'trailing'> {
  value: string
  onValueChange: (value: string) => void
  onClear?: () => void
  /** Right-side hint such as <Kbd>/</Kbd>; hidden once there is a value. */
  shortcut?: ReactNode
}

export function SearchInput({ value, onValueChange, onClear, shortcut, placeholder = 'Search…', size = 'md', className, ...rest }: SearchInputProps) {
  const clear = () => {
    onValueChange('')
    onClear?.()
  }
  return (
    <Input
      type="search"
      role="searchbox"
      autoComplete="off"
      spellCheck={false}
      size={size}
      leadingIcon={Search}
      value={value}
      onChange={(e) => onValueChange(e.target.value)}
      onKeyDown={(e) => {
        if (e.key === 'Escape' && value) {
          e.preventDefault()
          clear()
        }
      }}
      placeholder={placeholder}
      className={cn('[&_input::-webkit-search-cancel-button]:hidden', className)}
      trailing={
        value ? (
          <button type="button" aria-label="Clear search" onClick={clear} className="rounded p-1 text-fg-muted hover:bg-surface-hover hover:text-fg focus-ring">
            <X className="size-3.5" aria-hidden="true" />
          </button>
        ) : (
          shortcut ?? null
        )
      }
      {...rest}
    />
  )
}

export interface TextareaProps extends ComponentProps<'textarea'> {
  invalid?: boolean
  mono?: boolean
}

export function Textarea({ invalid, mono, className, rows = 3, ...rest }: TextareaProps) {
  return <textarea aria-invalid={invalid || undefined} rows={rows} className={cn(inputBase, 'px-3 py-2 text-sm leading-6', mono && 'font-mono', className)} {...rest} />
}

export interface FieldProps {
  label: ReactNode
  hint?: ReactNode
  error?: ReactNode
  required?: boolean
  /** id of the control; generated when omitted (pass `id` from the render prop). */
  htmlFor?: string
  className?: string
  children: ReactNode | ((ids: { id: string; describedBy: string | undefined }) => ReactNode)
  /** Put label and control side by side on wide screens. */
  horizontal?: boolean
}

/** Label + control + hint/error wrapper with aria wiring. */
export function Field({ label, hint, error, required, htmlFor, className, children, horizontal }: FieldProps) {
  const gen = useId()
  const id = htmlFor ?? gen
  const hintId = hint ? `${id}-hint` : undefined
  const errId = error ? `${id}-err` : undefined
  const describedBy = [hintId, errId].filter(Boolean).join(' ') || undefined
  return (
    <div className={cn(horizontal ? 'grid gap-2 sm:grid-cols-[minmax(0,1fr)_minmax(0,2fr)] sm:items-start sm:gap-6' : 'flex flex-col gap-1.5', className)}>
      <div>
        <label htmlFor={id} className="text-[13px] font-medium text-fg">
          {label}
          {required ? (
            <span className="ml-0.5 text-critical" aria-hidden="true">
              *
            </span>
          ) : null}
        </label>
        {horizontal && hint ? (
          <p id={hintId} className="mt-0.5 text-xs text-fg-muted">
            {hint}
          </p>
        ) : null}
      </div>
      <div className="flex flex-col gap-1.5">
        {typeof children === 'function' ? children({ id, describedBy }) : children}
        {!horizontal && hint && !error ? (
          <p id={hintId} className="text-xs text-fg-muted">
            {hint}
          </p>
        ) : null}
        {error ? (
          <p id={errId} role="alert" className="text-xs text-critical">
            {error}
          </p>
        ) : null}
      </div>
    </div>
  )
}
