// Interaction screenshots for the Alerts / Events / Settings pages (mock mode).
//
//   VITE_MOCK=1 npx vite --port 5173 --strictPort &
//   node scripts/screenshot-ops.cjs [--theme dark,light] [--viewport 1440x900,390x844] [--out screenshots]
//
// Captures states the plain route screenshots cannot reach: the Rules tab,
// the rule editor dialog, the Resolved tab, the event type filter popover,
// an expanded audit row and the Agent install tabs.

const path = require('node:path')
const fs = require('node:fs')
const { chromium } = require('@playwright/test')

function parseArgs(argv) {
  const out = { viewport: '1440x900,390x844', theme: 'dark,light', out: 'screenshots', base: 'http://localhost:5173', wait: '1500' }
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i]
    if (!a.startsWith('--')) continue
    out[a.slice(2)] = argv[++i]
  }
  return out
}

const SCENES = [
  {
    name: 'alerts-open',
    route: '/alerts',
    run: async () => {},
  },
  {
    name: 'alerts-resolved',
    route: '/alerts?tab=resolved',
    run: async () => {},
  },
  {
    name: 'alerts-rules',
    route: '/alerts?tab=rules',
    run: async () => {},
  },
  {
    name: 'alerts-rule-editor',
    route: '/alerts?tab=rules&rule=high_cpu',
    run: async (page) => {
      await page.waitForSelector('[role="dialog"]', { timeout: 5000 })
    },
  },
  {
    name: 'alerts-test-notification',
    route: '/alerts?tab=rules',
    run: async (page) => {
      await page.getByRole('button', { name: 'Send test notification' }).click()
      await page.waitForSelector('[role="dialog"]', { timeout: 5000 })
      await page.getByRole('button', { name: 'Send test', exact: true }).click()
      await page.waitForTimeout(800)
    },
  },
  { name: 'settings-hub', route: '/settings', element: '#hub' },
  { name: 'settings-auth', route: '/settings', element: '#auth' },
  { name: 'settings-sources', route: '/settings', element: '#sources' },
  { name: 'settings-storage', route: '/settings', element: '#storage' },
  { name: 'settings-notifications', route: '/settings', element: '#notifications' },
  { name: 'settings-audit', route: '/settings', element: '#audit' },
  {
    name: 'events-type-filter',
    route: '/events',
    run: async (page) => {
      await page.getByRole('button', { name: /Event types/ }).click()
      await page.waitForSelector('[role="dialog"]', { timeout: 5000 })
    },
  },
  {
    name: 'events-filtered',
    route: '/events?type=device.offline,device.online,alert.opened&since=7d',
    run: async () => {},
  },
  {
    // Scroll away from the top, then trigger a live event (acknowledge an
    // open alert through the mock API) so the "new events" pill appears.
    name: 'events-new-pill',
    route: '/events',
    run: async (page) => {
      await page.evaluate(() => window.scrollTo(0, 900))
      await page.waitForTimeout(300)
      const status = await page.evaluate(async () => {
        const alerts = await (await fetch('/api/v1/alerts?state=open')).json()
        const target = alerts.find((a) => !a.ackedAt)
        if (!target) return 'no-open-alert'
        const res = await fetch(`/api/v1/alerts/${target.id}/ack`, { method: 'POST', headers: { 'X-Requested-With': 'tailwatch' } })
        return String(res.status)
      })
      if (status !== '200') console.log(`  !! events-new-pill: ack returned ${status}`)
      await page.waitForSelector('text=/new event/', { timeout: 5000 })
    },
  },
  {
    name: 'settings-audit-expanded',
    route: '/settings#audit',
    run: async (page) => {
      await page.waitForSelector('#audit', { timeout: 5000 })
      const row = page.locator('#audit tbody tr[aria-expanded]').first()
      await row.click()
      await page.waitForTimeout(300)
      await page.locator('#audit').scrollIntoViewIfNeeded()
    },
  },
  {
    name: 'settings-agent-docker',
    route: '/settings#agent',
    run: async (page) => {
      await page.waitForSelector('#agent', { timeout: 5000 })
      await page.getByRole('switch', { name: 'Hub uses a shared token' }).click()
      await page.getByRole('tab', { name: 'Docker' }).click()
      await page.waitForTimeout(200)
      await page.locator('#agent').scrollIntoViewIfNeeded()
    },
  },
]

async function main() {
  const args = parseArgs(process.argv.slice(2))
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
        const context = await browser.newContext({ viewport: vp, deviceScaleFactor: 1, colorScheme: theme === 'dark' ? 'dark' : 'light', reducedMotion: 'reduce' })
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
        for (const scene of SCENES) {
          await page.goto(args.base.replace(/\/$/, '') + scene.route, { waitUntil: 'networkidle' })
          await page.waitForTimeout(Number(args.wait))
          try {
            if (scene.run) await scene.run(page)
          } catch (e) {
            console.log(`  !! ${scene.name}: ${String(e).split('\n')[0]}`)
          }
          await page.waitForTimeout(400)
          const file = path.join(outDir, `ops-${scene.name}-${theme}-${vp.width}x${vp.height}.png`)
          if (scene.element) await page.locator(scene.element).screenshot({ path: file })
          else await page.screenshot({ path: file, fullPage: false })
          const scrollW = await page.evaluate(() => document.documentElement.scrollWidth)
          console.log(`${file}${scrollW > vp.width ? `  !! horizontal overflow: ${scrollW} > ${vp.width}` : ''}`)
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
