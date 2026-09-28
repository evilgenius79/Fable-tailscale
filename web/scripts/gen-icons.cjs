// Renders public/icons/icon.svg to the PNG sizes the web app manifest needs.
// Run from web/: node scripts/gen-icons.cjs
// Uses the Chromium bundled with Playwright (executablePath override for the
// preinstalled browser in CI containers is optional).
const fs = require('node:fs')
const path = require('node:path')
const { chromium } = require('@playwright/test')

const ICON_DIR = path.join(__dirname, '..', 'public', 'icons')
const svg = fs.readFileSync(path.join(ICON_DIR, 'icon.svg'), 'utf8')

// Maskable icons must keep their content inside the inner 80% "safe zone";
// we render the same artwork scaled down on a solid background.
const targets = [
  { file: 'icon-192.png', size: 192, maskable: false },
  { file: 'icon-512.png', size: 512, maskable: false },
  { file: 'icon-maskable-192.png', size: 192, maskable: true },
  { file: 'icon-maskable-512.png', size: 512, maskable: true },
]

async function main() {
  const exe = process.env.PW_CHROMIUM || (fs.existsSync('/opt/pw-browsers/chromium') ? '/opt/pw-browsers/chromium' : undefined)
  const browser = await chromium.launch({ headless: true, executablePath: exe })
  const page = await browser.newPage({ deviceScaleFactor: 1 })
  for (const t of targets) {
    const inner = t.maskable ? Math.round(t.size * 0.78) : t.size
    const html = `<!doctype html><html><body style="margin:0;background:${t.maskable ? '#0a0a0c' : 'transparent'};">
      <div id="box" style="width:${t.size}px;height:${t.size}px;display:grid;place-items:center;background:${t.maskable ? '#0a0a0c' : 'transparent'}">
        <div style="width:${inner}px;height:${inner}px">${svg.replace('width="512" height="512"', `width="${inner}" height="${inner}"`)}</div>
      </div></body></html>`
    await page.setViewportSize({ width: t.size, height: t.size })
    await page.setContent(html)
    const el = await page.$('#box')
    await el.screenshot({ path: path.join(ICON_DIR, t.file), omitBackground: !t.maskable, type: 'png' })
    console.log('wrote', t.file)
  }
  await browser.close()
}

main().catch((err) => {
  console.error(err)
  process.exit(1)
})
