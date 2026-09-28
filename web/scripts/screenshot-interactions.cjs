#!/usr/bin/env node
/* eslint-disable */
// Interaction screenshots for the Devices page (popovers, density, keyboard nav).
//   cd web && node scripts/screenshot-interactions.cjs [--base http://localhost:5173] [--out screenshots]
const path = require('node:path')
const fs = require('node:fs')
const { chromium } = require('@playwright/test')

const args = Object.fromEntries(process.argv.slice(2).map((a, i, arr) => (a.startsWith('--') ? [a.slice(2), arr[i + 1]] : [])).filter((x) => x.length))
const base = args.base ?? 'http://localhost:5173'
const outDir = path.resolve(process.cwd(), args.out ?? 'screenshots')
fs.mkdirSync(outDir, { recursive: true })

async function shot(page, name) {
  const file = path.join(outDir, `${name}.png`)
  await page.screenshot({ path: file })
  const scrollW = await page.evaluate(() => document.documentElement.scrollWidth)
  const vw = page.viewportSize().width
  console.log(`${file}${scrollW > vw ? `  !! horizontal overflow ${scrollW} > ${vw}` : ''}`)
}

async function run(theme, viewport, steps) {
  const browser = await chromium.launch({ executablePath: '/opt/pw-browsers/chromium' })
  const context = await browser.newContext({ viewport, deviceScaleFactor: 1, colorScheme: theme, reducedMotion: 'reduce' })
  await context.addInitScript((t) => {
    try {
      localStorage.setItem('tailwatch.ui', JSON.stringify({ state: { theme: t, sidebarCollapsed: false }, version: 1 }))
    } catch {}
  }, theme)
  const page = await context.newPage()
  const errors = []
  page.on('pageerror', (e) => errors.push(String(e)))
  page.on('console', (m) => m.type() === 'error' && errors.push(m.text()))
  await steps(page)
  if (errors.length) console.log('  console errors:\n    ' + errors.join('\n    '))
  await browser.close()
}

async function main() {
  await run('dark', { width: 1440, height: 900 }, async (page) => {
    await page.goto(base + '/devices', { waitUntil: 'networkidle' })
    await page.waitForTimeout(1500)
    await page.getByRole('button', { name: /^Filters/ }).click()
    await page.waitForTimeout(300)
    await shot(page, 'ix-devices-filters-open-dark')
    await page.keyboard.press('Escape')
    await page.getByRole('button', { name: 'Choose columns' }).click()
    await page.waitForTimeout(300)
    await shot(page, 'ix-devices-columns-open-dark')
    await page.keyboard.press('Escape')
    await page.getByRole('radio', { name: 'Compact rows' }).click()
    await page.waitForTimeout(300)
    // keyboard: focus the first row and move down twice
    await page.locator('tbody tr[tabindex]').first().focus()
    await page.keyboard.press('ArrowDown')
    await page.keyboard.press('ArrowDown')
    await page.waitForTimeout(200)
    const focused = await page.evaluate(() => document.activeElement?.getAttribute('aria-label'))
    console.log('  focused row after ArrowDown x2:', focused)
    await shot(page, 'ix-devices-compact-keyboard-dark')
    await page.getByRole('button', { name: /^Sort by/ }).click()
    await page.waitForTimeout(300)
    await shot(page, 'ix-devices-sort-menu-dark')
    await page.keyboard.press('Escape')
    // live sparklines after a few ticks
    await page.waitForTimeout(12000)
    await shot(page, 'ix-devices-live-sparklines-dark')
  })
  await run('light', { width: 390, height: 844 }, async (page) => {
    await page.goto(base + '/devices', { waitUntil: 'networkidle' })
    await page.waitForTimeout(1500)
    await page.getByRole('button', { name: /^Filters/ }).click()
    await page.waitForTimeout(300)
    await shot(page, 'ix-devices-filters-open-mobile-light')
    await page.keyboard.press('Escape')
    await page.getByRole('button', { name: /^Sort by/ }).click()
    await page.waitForTimeout(300)
    await shot(page, 'ix-devices-sort-menu-mobile-light')
  })
  await run('dark', { width: 390, height: 844 }, async (page) => {
    await page.goto(base + '/', { waitUntil: 'networkidle' })
    await page.waitForTimeout(1500)
    await page.getByRole('radio', { name: 'Latency' }).click()
    await page.waitForTimeout(300)
    const card = page.getByText('Highest latency online devices')
    await card.scrollIntoViewIfNeeded()
    await shot(page, 'ix-home-top-latency-mobile-dark')
  })
  await run('light', { width: 1024, height: 768 }, async (page) => {
    await page.goto(base + '/devices', { waitUntil: 'networkidle' })
    await page.waitForTimeout(1500)
    await shot(page, 'ix-devices-1024-light')
    await page.goto(base + '/', { waitUntil: 'networkidle' })
    await page.waitForTimeout(1500)
    await shot(page, 'ix-home-1024-light')
  })
  await run('dark', { width: 1920, height: 1080 }, async (page) => {
    await page.goto(base + '/devices', { waitUntil: 'networkidle' })
    await page.waitForTimeout(1500)
    await shot(page, 'ix-devices-1920-dark')
  })
}
main().catch((e) => {
  console.error(e)
  process.exit(1)
})
