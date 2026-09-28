import type { ComponentProps, ReactNode } from 'react'
import type { LucideIcon } from 'lucide-react'
import { cn } from '../../lib/cn'
import { Spinner } from './Spinner'
import { Tooltip } from './Tooltip'

export type ButtonVariant = 'primary' | 'secondary' | 'outline' | 'ghost' | 'danger' | 'link'
export type ButtonSize = 'xs' | 'sm' | 'md' | 'lg'

const BASE =
  'inline-flex items-center justify-center gap-1.5 whitespace-nowrap rounded-md font-medium select-none transition-colors focus-ring disabled:opacity-50 disabled:pointer-events-none'

const VARIANTS: Record<ButtonVariant, string> = {
  primary: 'bg-accent text-accent-fg shadow-xs hover:bg-accent-hover active:bg-accent-active',
  secondary: 'bg-surface-raised text-fg border border-border shadow-xs hover:bg-surface-hover hover:border-border-strong active:bg-surface-active',
  outline: 'bg-transparent text-fg border border-border-strong hover:bg-surface-hover active:bg-surface-active',
  ghost: 'bg-transparent text-fg-secondary hover:bg-surface-hover hover:text-fg active:bg-surface-active',
  danger: 'bg-critical-fill text-white dark:text-fg-inverted shadow-xs hover:brightness-110 active:brightness-95',
  link: 'bg-transparent text-accent-text hover:underline underline-offset-4 h-auto px-0',
}

const SIZES: Record<ButtonSize, string> = {
  xs: 'h-7 px-2 text-xs [&_svg]:size-3.5',
  sm: 'h-8 px-2.5 text-[13px] [&_svg]:size-4',
  md: 'h-9 px-3 text-sm [&_svg]:size-4',
  lg: 'h-10 px-4 text-sm [&_svg]:size-[18px]',
}

const ICON_SIZES: Record<ButtonSize, string> = {
  xs: 'size-7 [&_svg]:size-3.5',
  sm: 'size-8 [&_svg]:size-4',
  md: 'size-9 [&_svg]:size-4',
  lg: 'size-10 [&_svg]:size-[18px]',
}

export interface ButtonClassOptions {
  variant?: ButtonVariant
  size?: ButtonSize
  block?: boolean
  iconOnly?: boolean
}

/** Class string for button-styled elements (e.g. `<Link className={buttonClass({ variant: 'primary' })}>`). */
export function buttonClass({ variant = 'secondary', size = 'md', block, iconOnly }: ButtonClassOptions = {}): string {
  return cn(BASE, VARIANTS[variant], variant === 'link' ? 'text-sm' : iconOnly ? ICON_SIZES[size] : SIZES[size], block && 'w-full')
}

export interface ButtonProps extends ComponentProps<'button'> {
  variant?: ButtonVariant
  size?: ButtonSize
  loading?: boolean
  leadingIcon?: LucideIcon
  trailingIcon?: LucideIcon
  block?: boolean
}

export function Button({
  variant = 'secondary',
  size = 'md',
  loading = false,
  leadingIcon: Leading,
  trailingIcon: Trailing,
  block,
  className,
  children,
  disabled,
  type = 'button',
  ...rest
}: ButtonProps) {
  return (
    <button
      type={type}
      className={cn(buttonClass({ variant, size, block }), className)}
      disabled={disabled || loading}
      aria-busy={loading || undefined}
      {...rest}
    >
      {loading ? <Spinner size="xs" /> : Leading ? <Leading aria-hidden="true" /> : null}
      {children}
      {Trailing && !loading ? <Trailing aria-hidden="true" /> : null}
    </button>
  )
}

export interface IconButtonProps extends Omit<ComponentProps<'button'>, 'children'> {
  icon: LucideIcon
  /** Accessible name; also the tooltip text. */
  label: string
  variant?: Exclude<ButtonVariant, 'link'>
  size?: ButtonSize
  active?: boolean
  loading?: boolean
  /** Show the label as a tooltip on hover/focus (default true). */
  tooltip?: boolean
  tooltipSide?: 'top' | 'bottom' | 'left' | 'right'
  children?: ReactNode
}

export function IconButton({
  icon: Icon,
  label,
  variant = 'ghost',
  size = 'md',
  active,
  loading,
  tooltip = true,
  tooltipSide = 'bottom',
  className,
  disabled,
  type = 'button',
  children,
  ...rest
}: IconButtonProps) {
  const btn = (
    <button
      type={type}
      aria-label={label}
      aria-pressed={active}
      className={cn(buttonClass({ variant, size, iconOnly: true }), active && 'bg-surface-active text-fg', 'relative', className)}
      disabled={disabled || loading}
      aria-busy={loading || undefined}
      {...rest}
    >
      {loading ? <Spinner size="xs" /> : <Icon aria-hidden="true" />}
      {children}
    </button>
  )
  return tooltip ? (
    <Tooltip content={label} side={tooltipSide}>
      {btn}
    </Tooltip>
  ) : (
    btn
  )
}
