#!/usr/bin/env node
/* eslint-disable */
// Screenshot interactive states of the device detail and network pages
// (Manage menu, dialogs, ping result, topology hover tooltip) against the
// mock dev server. Run from web/ with `VITE_MOCK=1 npx vite --port 5173` up:
//
//   node scripts/ui-states.cjs [--theme dark] [--viewport 1440x900] [--out screenshots]

const path = require('node:path')
const fs = require('node:fs')
const { chromium } = require('@playwright/test')

function arg(name, def) {
  const i = process.argv.indexOf(`--${name}`)
  return i === -1 ? def : process.argv[i + 1]
}

async function main() {
  const theme = arg('theme', 'dark')
  const [w, h] = arg('viewport', '1440x900').split('x').map(Number)
  const outDir = path.resolve(process.cwd(), arg('out', 'screenshots'))
  const base = arg('base', 'http://localhost:5173')
  fs.mkdirSync(outDir, { recursive: true })
  const browser = await chromium.launch({ executablePath: '/opt/pw-browsers/chromium' })
  const context = await browser.newContext({ viewport: { width: w, height: h }, deviceScaleFactor: 1, colorScheme: theme, reducedMotion: 'reduce' })
  await context.addInitScript((t) => {
    try {
      localStorage.setItem('tailwatch.ui', JSON.stringify({ state: { theme: t, sidebarCollapsed: false }, version: 1 }))
    } catch {}
  }, theme)
  const page = await context.newPage()
  const errors = []
  page.on('pageerror', (e) => errors.push(String(e)))
  page.on('console', (m) => m.type() === 'error' && errors.push(m.text()))
  const shot = (name) => page.screenshot({ path: path.join(outDir, `state-${name}-${theme}-${w}x${h}.png`) })

  // --- Device detail: ping result ------------------------------------------
  await page.goto(`${base}/devices/nB2c9Dnas0`, { waitUntil: 'networkidle' })
  await page.getByRole('button', { name: /^Ping nas-basement/ }).click()
  await page.locator('div[role="status"]').filter({ hasText: /ms|Ping failed/ }).waitFor({ timeout: 8000 })
  await page.waitForTimeout(300)
  await shot('ping')

  // --- Manage menu ---------------------------------------------------------
  await page.getByRole('button', { name: 'Manage' }).click()
  await page.getByRole('menu').waitFor()
  await shot('manage-menu')

  // --- Tags dialog ---------------------------------------------------------
  await page.getByRole('menuitem', { name: 'Edit tags…' }).click()
  await page.getByRole('dialog').waitFor()
  await page.getByPlaceholder('tag:server').fill('backup')
  await page.getByRole('button', { name: 'Add' }).click()
  await page.getByPlaceholder('tag:server').fill('bad tag!')
  await page.getByRole('button', { name: 'Add' }).click()
  await page.waitForTimeout(200)
  await shot('tags-dialog')
  await page.keyboard.press('Escape')

  // --- Routes dialog -------------------------------------------------------
  await page.getByRole('button', { name: 'Manage' }).click()
  await page.getByRole('menuitem', { name: 'Approve routes…' }).click()
  await page.getByRole('dialog').waitFor()
  await shot('routes-dialog')
  await page.keyboard.press('Escape')

  // --- Rename dialog -------------------------------------------------------
  await page.getByRole('button', { name: 'Manage' }).click()
  await page.getByRole('menuitem', { name: 'Rename…' }).click()
  await page.getByRole('dialog').waitFor()
  await page.getByLabel('Machine name').fill('Nas Basement')
  await page.waitForTimeout(200)
  await shot('rename-dialog')
  await page.keyboard.press('Escape')

  // --- Delete dialog -------------------------------------------------------
  await page.getByRole('button', { name: 'Manage' }).click()
  await page.getByRole('menuitem', { name: 'Remove from tailnet…' }).click()
  await page.getByRole('dialog').waitFor()
  await page.getByRole('textbox').fill('nas-basement')
  await page.waitForTimeout(200)
  await shot('delete-dialog')
  await page.keyboard.press('Escape')

  // --- Rollup range (7d) with max envelope ---------------------------------
  await page.getByRole('radio', { name: 'Last 7 days' }).click()
  await page.waitForTimeout(1500)
  await shot('range-7d')

  // --- Chart hover (synced crosshair) -------------------------------------
  const chart = page.locator('.recharts-surface').first()
  const box = await chart.boundingBox()
  if (box) {
    await page.mouse.move(box.x + box.width * 0.6, box.y + box.height * 0.5)
    await page.waitForTimeout(400)
    await shot('chart-hover')
  }

  // --- Availability outage tooltip (flapper) ------------------------------
  await page.goto(`${base}/devices/nB6k7Pbkp0`, { waitUntil: 'networkidle' })
  await page.waitForTimeout(1500)
  const outage = page.locator('button[aria-label^="Offline"]').first()
  if (await outage.count()) {
    await outage.hover()
    await page.waitForTimeout(500)
  }
  await shot('flapper-timeline')

  // --- Unauthorized device header -----------------------------------------
  await page.goto(`${base}/devices/nN8w9Unew8`, { waitUntil: 'networkidle' })
  await page.waitForTimeout(1200)
  await shot('unauthorized')

  // --- Agent unreachable (System tab) -------------------------------------
  await page.goto(`${base}/devices/nB0b1Ttpd3?tab=system`, { waitUntil: 'networkidle' })
  await page.waitForTimeout(1200)
  await shot('agent-unreachable')

  // --- Not found -----------------------------------------------------------
  await page.goto(`${base}/devices/does-not-exist`, { waitUntil: 'networkidle' })
  await page.waitForTimeout(1200)
  await shot('not-found')

  // --- Network: hover tooltip + filters -----------------------------------
  await page.goto(`${base}/network`, { waitUntil: 'networkidle' })
  await page.waitForTimeout(3500)
  const node = page.locator('[data-node-id="nP4i8Ggar1"]')
  await node.hover()
  await page.waitForTimeout(400)
  await shot('map-hover')
  await page.getByRole('radio', { name: 'Relayed' }).click()
  await page.waitForTimeout(3000)
  await shot('map-relay-filter')
  await page.getByRole('radio', { name: 'All paths' }).click()
  await page.getByRole('switch', { name: 'Online only' }).click()
  await page.waitForTimeout(3000)
  await page.getByRole('button', { name: 'Zoom in' }).click()
  await page.getByRole('button', { name: 'Zoom in' }).click()
  await page.waitForTimeout(400)
  await shot('map-zoomed')

  if (errors.length) console.log('console errors:\n  ' + errors.join('\n  '))
  else console.log('no console errors')
  await browser.close()
}

main().catch((e) => {
  console.error(e)
  process.exit(1)
})
