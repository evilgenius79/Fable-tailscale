#!/usr/bin/env node
/* eslint-disable */
// Assert the Devices table's "Auto" column set never overflows its scroll
// container (which would clip the last column behind a barely visible
// horizontal scrollbar). The auto breakpoints in src/components/devices/
// columns.ts are container queries against that container, so this measures
// clientWidth vs scrollWidth at the common viewports in both densities.
//
//   cd web && VITE_MOCK=1 npx vite --port 5175 --strictPort &
//   node scripts/check-table-fit.cjs --base http://127.0.0.1:5175
//
// Options: --base (default http://localhost:5173), --viewports (comma-separated widths).

const { chromium } = require('@playwright/test')

function parseArgs(argv) {
  const out = { base: 'http://localhost:5173', viewports: '1024,1100,1280,1366,1440,1536,1600,1680,1920' }
  for (let i = 0; i < argv.length; i++) if (argv[i].startsWith('--')) out[argv[i].slice(2)] = argv[++i]
  return out
}

async function main() {
  const args = parseArgs(process.argv.slice(2))
  const widths = args.viewports.split(',').map(Number).filter(Boolean)
  const browser = await chromium.launch({ executablePath: process.env.PW_CHROMIUM || '/opt/pw-browsers/chromium' })
  let failures = 0
  try {
    for (const density of ['comfortable', 'compact']) {
      for (const width of widths) {
        const ctx = await browser.newContext({ viewport: { width, height: 900 }, colorScheme: 'dark' })
        await ctx.addInitScript((d) => localStorage.setItem('tailwatch.devices', JSON.stringify({ density: d, columns: null })), density)
        const page = await ctx.newPage()
        await page.goto(`${args.base}/devices`, { waitUntil: 'networkidle' })
        await page.waitForSelector('table[aria-label="Devices"] tbody tr[tabindex]', { timeout: 15000 })
        await page.waitForTimeout(500)
        const r = await page.evaluate(() => {
          const table = document.querySelector('table[aria-label="Devices"]')
          const c = table.parentElement
          const cols = Array.from(table.querySelectorAll('thead th'))
            .filter((th) => th.getClientRects().length)
            .map((th) => `${th.textContent.trim()}:${Math.round(th.getBoundingClientRect().width)}`)
          return { clientW: c.clientWidth, scrollW: c.scrollWidth, cols }
        })
        const ok = r.scrollW <= r.clientW
        if (!ok) failures++
        console.log(`${ok ? 'ok  ' : 'FAIL'} ${density.padEnd(11)} ${String(width).padStart(4)}px  container=${r.clientW} table=${r.scrollW}  [${r.cols.join(' ')}]`)
        await ctx.close()
      }
    }
  } finally {
    await browser.close()
  }
  if (failures) {
    console.error(`${failures} viewport(s) overflow`)
    process.exit(1)
  }
}

main().catch((e) => {
  console.error(e)
  process.exit(1)
})
