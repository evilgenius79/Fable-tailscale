// Chart tokens resolved from the CSS custom properties in index.css so SVG
// attributes (which cannot use var()) get concrete colours for the active theme.

import { useMemo } from 'react'
import { useUIStore, selectResolvedTheme } from '../../store'
import type { StatusTone } from '../../lib/status'

export interface ChartTheme {
  mode: 'light' | 'dark'
  /** Eight categorical slots in fixed order (validated palette). Never cycle past 8. */
  series: readonly string[]
  grid: string
  axis: string
  text: string
  textMuted: string
  surface: string
  surfaceRaised: string
  border: string
  accent: string
  /** De-emphasis colour for context series / "Other". */
  muted: string
  status: Record<Exclude<StatusTone, 'neutral' | 'accent'>, string>
  font: string
  fontSize: number
}

const FALLBACK_DARK: ChartTheme = {
  mode: 'dark',
  series: ['#4f8ef7', '#e0602a', '#1fae7e', '#c98500', '#d9558a', '#20953f', '#9085e9', '#e66767'],
  grid: '#1f1f25',
  axis: '#303039',
  text: '#f4f4f5',
  textMuted: '#7d7d89',
  surface: '#121216',
  surfaceRaised: '#18181d',
  border: '#232329',
  accent: '#3b7ff0',
  muted: '#3a3a44',
  status: { online: '#34d399', offline: '#6b6b76', warning: '#fbbf24', critical: '#fb7185', relay: '#a78bfa', direct: '#34d399', info: '#38bdf8' },
  font: "'Inter Variable', ui-sans-serif, system-ui, sans-serif",
  fontSize: 11,
}

const FALLBACK_LIGHT: ChartTheme = {
  ...FALLBACK_DARK,
  mode: 'light',
  series: ['#2f6fe4', '#ea580c', '#0f9f7a', '#d99a00', '#db5c8e', '#15803d', '#6d4fd6', '#dc3d3d'],
  grid: '#ececf0',
  axis: '#d2d2d9',
  text: '#18181b',
  textMuted: '#71717a',
  surface: '#ffffff',
  surfaceRaised: '#ffffff',
  border: '#e5e5ea',
  accent: '#2f6fe4',
  muted: '#c4c4cc',
  status: { online: '#059669', offline: '#a1a1aa', warning: '#d97706', critical: '#e11d48', relay: '#7c3aed', direct: '#059669', info: '#0284c7' },
}

/** Read the live tokens from :root / .dark. Falls back to the static palette when not in a browser. */
export function readChartTheme(mode: 'light' | 'dark'): ChartTheme {
  const fallback = mode === 'dark' ? FALLBACK_DARK : FALLBACK_LIGHT
  if (typeof window === 'undefined' || typeof getComputedStyle !== 'function') return fallback
  const cs = getComputedStyle(document.documentElement)
  const v = (name: string, fb: string) => cs.getPropertyValue(name).trim() || fb
  return {
    mode,
    series: fallback.series.map((fb, i) => v(`--chart-${i + 1}`, fb)),
    grid: v('--chart-grid', fallback.grid),
    axis: v('--chart-axis', fallback.axis),
    text: v('--fg', fallback.text),
    textMuted: v('--fg-muted', fallback.textMuted),
    surface: v('--surface', fallback.surface),
    surfaceRaised: v('--surface-raised', fallback.surfaceRaised),
    border: v('--border', fallback.border),
    accent: v('--accent', fallback.accent),
    muted: v('--chart-muted', fallback.muted),
    status: {
      online: v('--online-fill', fallback.status.online),
      offline: v('--offline-fill', fallback.status.offline),
      warning: v('--warning-fill', fallback.status.warning),
      critical: v('--critical-fill', fallback.status.critical),
      relay: v('--relay-fill', fallback.status.relay),
      direct: v('--direct-fill', fallback.status.direct),
      info: v('--info-fill', fallback.status.info),
    },
    font: fallback.font,
    fontSize: 11,
  }
}

/** Chart tokens for the active theme (re-read when the theme toggles). */
export function useChartTheme(): ChartTheme {
  const mode = useUIStore(selectResolvedTheme)
  return useMemo(() => readChartTheme(mode), [mode])
}

/** Categorical slot colour by index (fixed order). Past slot 8 returns the muted "Other" colour. */
export function seriesColor(theme: ChartTheme, index: number): string {
  return theme.series[index] ?? theme.muted
}

/** Colour for a status tone in chart space. */
export function toneColor(theme: ChartTheme, tone: StatusTone): string {
  if (tone === 'accent') return theme.accent
  if (tone === 'neutral') return theme.muted
  return theme.status[tone]
}

/** Shared mark specs (dataviz): 2px lines, ≥8px markers, 10% area wash, hairline grid. */
export const MARK = {
  lineWidth: 2,
  markerRadius: 4,
  areaOpacity: 0.1,
  gridWidth: 1,
} as const

export const CHART_MARGIN = { top: 8, right: 8, bottom: 0, left: 0 } as const
