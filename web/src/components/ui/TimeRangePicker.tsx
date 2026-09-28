import { ChevronDown, Clock } from 'lucide-react'
import { cn } from '../../lib/cn'
import { DEFAULT_RANGE_OPTIONS, RANGE_PRESETS, rangePreset, type RangeKey } from '../../lib/time'
import { Button } from './Button'
import { DropdownMenu } from './Menu'
import { SegmentedControl } from './SegmentedControl'

export interface TimeRangePickerProps {
  value: RangeKey
  onValueChange: (range: RangeKey) => void
  /** Presets for the segmented variant (the menu always lists every preset). */
  options?: ReadonlyArray<RangeKey>
  /** 'auto' = segmented from the sm breakpoint, menu below it. */
  variant?: 'segmented' | 'menu' | 'auto'
  size?: 'xs' | 'sm' | 'md'
  className?: string
  'aria-label'?: string
}

/**
 * Global time range control. Presets are rows in a menu (with a check on the
 * selection) or a compact segmented control — never a calendar grid.
 */
export function TimeRangePicker({ value, onValueChange, options = DEFAULT_RANGE_OPTIONS, variant = 'auto', size = 'sm', className, ...aria }: TimeRangePickerProps) {
  const label = aria['aria-label'] ?? 'Time range'
  // If the current value is not in the compact options, show it as an extra segment.
  const segs = options.includes(value) ? options : [...options, value]
  const segmented = (
    <SegmentedControl
      aria-label={label}
      size={size}
      value={value}
      onValueChange={onValueChange}
      options={segs.map((k) => ({ value: k, label: rangePreset(k).label, ariaLabel: rangePreset(k).longLabel }))}
    />
  )
  const menu = (
    <DropdownMenu
      label={label}
      align="end"
      width={200}
      items={RANGE_PRESETS.map((p) => ({ id: p.key, label: p.longLabel, checked: p.key === value, onSelect: () => onValueChange(p.key) }))}
      trigger={
        <Button variant="secondary" size={size === 'xs' ? 'xs' : size === 'sm' ? 'sm' : 'md'} leadingIcon={Clock} trailingIcon={ChevronDown} aria-label={`${label}: ${rangePreset(value).longLabel}`}>
          {rangePreset(value).longLabel}
        </Button>
      }
    />
  )
  if (variant === 'segmented') return <div className={cn('inline-flex', className)}>{segmented}</div>
  if (variant === 'menu') return <div className={cn('inline-flex', className)}>{menu}</div>
  return (
    <div className={cn('inline-flex', className)}>
      <div className="hidden sm:inline-flex">{segmented}</div>
      <div className="sm:hidden">{menu}</div>
    </div>
  )
}
