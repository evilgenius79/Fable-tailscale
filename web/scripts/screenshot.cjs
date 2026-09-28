#!/usr/bin/env node
/* eslint-disable */
// Screenshot routes of the running dev server in one or more viewports/themes.
//
//   cd web && VITE_MOCK=1 npx vite --port 5173 --strictPort &
//   node scripts/screenshot.cjs --routes /,/devices --viewport 1440x900,390x844 --theme dark,light
//
// Options:
//   --routes    comma-separated paths (default: /)
//   --viewport  comma-separated WxH (default: 1440x900)
//   --theme     comma-separated dark|light (default: dark)
//   --out       output dir (default: screenshots)
//   --base      server origin (default: http://localhost:5173)
//   --full      full-page screenshots
//   --wait      extra ms to wait after load (default: 1200)
//   --prefix    filename prefix
//
// Files: <out>/<prefix><route-slug>-<theme>-<WxH>.png

const path = require('node:path')
const fs = require('node:fs')
const { chromium } = require('@playwright/test')

function parseArgs(argv) {
  const out = { routes: '/', viewport: '1440x900', theme: 'dark', out: 'screenshots', base: 'http://localhost:5173', full: false, wait: '1200', prefix: '' }
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i]
    if (!a.startsWith('--')) continue
    const key = a.slice(2)
    if (key === 'full') {
      out.full = true
      continue
    }
    out[key] = argv[++i]
  }
  return out
}

function slug(route) {
  const s = route.replace(/^\//, '').replace(/[^a-z0-9]+/gi, '-').replace(/^-|-$/g, '')
  return s || 'home'
}

async function main() {
  const args = parseArgs(process.argv.slice(2))
  const routes = args.routes.split(',').map((s) => s.trim()).filter(Boolean)
  const viewports = args.viewport.split(',').map((v) => {
    const [w, h] = v.toLowerCase().split('x').map(Number)
    return { width: w, height: h }
  })
  const themes = args.theme.split(',').map((s) => s.trim()).filter(Boolean)
  const outDir = path.resolve(process.cwd(), args.out)
  fs.mkdirSync(outDir, { recursive: true })

  const browser = await chromium.launch({ executablePath: '/opt/pw-browsers/chromium' })
  try {
    for (const theme of themes) {
      for (const vp of viewports) {
        const context = await browser.newContext({
          viewport: vp,
          deviceScaleFactor: 1,
          colorScheme: theme === 'dark' ? 'dark' : 'light',
          reducedMotion: 'reduce',
        })
        // Persisted UI preference (same shape as the zustand persist store).
        await context.addInitScript((t) => {
          try {
            localStorage.setItem('tailwatch.ui', JSON.stringify({ state: { theme: t, sidebarCollapsed: false }, version: 1 }))
          } catch {}
        }, theme)
        const page = await context.newPage()
        const errors = []
        page.on('pageerror', (e) => errors.push(String(e)))
        page.on('console', (m) => {
          if (m.type() === 'error') errors.push(m.text())
        })
        for (const route of routes) {
          const url = args.base.replace(/\/$/, '') + route
          await page.goto(url, { waitUntil: 'networkidle' })
          await page.waitForTimeout(Number(args.wait))
          const file = path.join(outDir, `${args.prefix}${slug(route)}-${theme}-${vp.width}x${vp.height}.png`)
          await page.screenshot({ path: file, fullPage: args.full })
          const scrollW = await page.evaluate(() => document.documentElement.scrollWidth)
          const overflow = scrollW > vp.width ? `  !! horizontal overflow: scrollWidth ${scrollW} > ${vp.width}` : ''
          console.log(`${file}${overflow}`)
        }
        if (errors.length) console.log(`  console errors (${theme} ${vp.width}x${vp.height}):\n    ` + errors.join('\n    '))
        await context.close()
      }
    }
  } finally {
    await browser.close()
  }
}

main().catch((e) => {
  console.error(e)
  process.exit(1)
})
