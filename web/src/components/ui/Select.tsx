import type { ComponentProps } from 'react'
import { ChevronDown } from 'lucide-react'
import { cn } from '../../lib/cn'
import { inputBase, type InputSize } from './Input'

export interface SelectOption<T extends string = string> {
  value: T
  label: string
  disabled?: boolean
}

export interface SelectProps<T extends string = string> extends Omit<ComponentProps<'select'>, 'value' | 'onChange' | 'size'> {
  value: T
  onValueChange: (value: T) => void
  options: ReadonlyArray<SelectOption<T>>
  size?: InputSize
  placeholder?: string
  invalid?: boolean
}

const SIZES: Record<InputSize, string> = { sm: 'h-8 text-[13px] pl-2.5 pr-8', md: 'h-9 text-sm pl-3 pr-9', lg: 'h-10 text-sm pl-3 pr-9' }

/** Native <select> with consistent chrome — the most accessible option on every platform. */
export function Select<T extends string = string>({ value, onValueChange, options, size = 'md', placeholder, invalid, className, ...rest }: SelectProps<T>) {
  return (
    <div className={cn('relative inline-flex items-center', className)}>
      <select
        value={value}
        onChange={(e) => onValueChange(e.target.value as T)}
        aria-invalid={invalid || undefined}
        className={cn(inputBase, 'appearance-none cursor-pointer', SIZES[size])}
        {...rest}
      >
        {placeholder ? (
          <option value="" disabled>
            {placeholder}
          </option>
        ) : null}
        {options.map((o) => (
          <option key={o.value} value={o.value} disabled={o.disabled}>
            {o.label}
          </option>
        ))}
      </select>
      <ChevronDown className="pointer-events-none absolute right-2.5 size-4 text-fg-muted" aria-hidden="true" />
    </div>
  )
}
