'use strict'

const fs = require('node:fs')
const path = require('node:path')
const assert = require('node:assert/strict')
const { chromium } = require('/workspace/agenteam-d26-auth-author-jrfhr6h1/input/tests/account-captcha-web/node_modules/playwright')

const root = __dirname
const result = {
  scope: 'native local DOM only; not Vue/application/API verification',
  playwright: require('/workspace/agenteam-d26-auth-author-jrfhr6h1/input/tests/account-captcha-web/node_modules/playwright/package.json').version,
  executable: '/usr/lib/chromium/chromium',
  pageRequests: [],
  cases: [],
  contextCloseReturned: false,
}
const save = () => fs.writeFileSync(path.join(root, 'evidence', 'observations.json'), JSON.stringify(result, null, 2) + '\n')
let context

async function setup(page, mode) {
  await page.setContent('<!doctype html><html><head><meta charset="utf-8"><title>Local focus observation</title></head><body><section id="question"><label for="angle">Angle</label><input id="angle" type="range" min="0" max="360"><button id="verify" type="button">Verify</button></section><label for="external">External control</label><input id="external" type="text"></body></html>')
  await page.evaluate((mode) => {
    const source = document.getElementById(mode === 'button' ? 'verify' : 'angle')
    const question = document.getElementById('question')
    const state = (window.focusProbe = { mode, events: [], snapshots: [], done: false })
    const label = (element) => element ? element.id || element.tagName.toLowerCase() : null
    const snapshot = (stage) => state.snapshots.push({
      stage,
      active: label(document.activeElement),
      disabled: source.disabled,
      questionOwnsActive: question.contains(document.activeElement),
    })
    for (const name of ['blur', 'focusout', 'focus', 'focusin']) {
      document.addEventListener(name, (event) => state.events.push({
        type: event.type, target: label(event.target), related: label(event.relatedTarget),
      }), true)
    }
    function disable() {
      snapshot('before-disable')
      source.disabled = true
      snapshot('after-disable-sync')
      queueMicrotask(() => snapshot('after-disable-microtask'))
      requestAnimationFrame(() => { snapshot('after-disable-frame'); state.done = true })
    }
    if (mode === 'range') source.addEventListener('keydown', (event) => {
      if (event.key === 'Enter') { event.preventDefault(); disable() }
    })
    else if (mode === 'button') source.addEventListener('click', disable)
    else window.disableUnfocusedQuestion = disable
  }, mode)
}

async function main() {
  assert.equal(result.playwright, '1.56.1')
  context = await chromium.launchPersistentContext(path.join(root, 'runtime', 'profile'), {
    executablePath: result.executable,
    headless: true,
    viewport: { width: 640, height: 480 },
    timeout: 15000,
    args: ['--disable-background-networking', '--disable-component-update', '--disable-domain-reliability'],
  })
  await context.setOffline(true)
  await context.route('**/*', (route) => route.abort())
  context.on('request', (request) => result.pageRequests.push({ method: request.method(), url: request.url() }))
  context.setDefaultTimeout(5000)
  const page = context.pages()[0] || await context.newPage()
  assert.equal(page.url(), 'about:blank')
  const cdp = await context.newCDPSession(page)
  result.browserVersion = await cdp.send('Browser.getVersion')
  await cdp.detach()

  for (const mode of ['range', 'button', 'external-control']) {
    await setup(page, mode)
    if (mode === 'range') {
      await page.locator('#angle').focus()
      await page.keyboard.press('Enter')
    } else if (mode === 'button') {
      await page.locator('#verify').click()
    } else {
      await page.locator('#external').focus()
      await page.evaluate(() => window.disableUnfocusedQuestion())
    }
    await page.waitForFunction(() => window.focusProbe.done)
    const observation = await page.evaluate(() => window.focusProbe)
    result.cases.push(observation)
    save()
  }
  for (const observation of result.cases) {
    const first = observation.snapshots[0]
    const last = observation.snapshots.at(-1)
    if (observation.mode === 'external-control') {
      assert.equal(first.active, 'external')
      assert.equal(last.active, 'external')
      assert.equal(last.disabled, true)
    } else {
      const source = observation.mode === 'range' ? 'angle' : 'verify'
      assert.equal(first.active, source)
      assert.equal(first.questionOwnsActive, true)
      assert.equal(last.active, 'body')
      assert.equal(last.disabled, true)
      assert.equal(last.questionOwnsActive, false)
      assert.ok(observation.events.some((event) => event.type === 'blur' && event.target === source))
    }
  }
  assert.equal(result.pageRequests.length, 0)
  result.result = 'PASS'
}

main().catch((error) => {
  result.result = 'FAIL'
  result.error = { name: error.name, message: error.message, stack: error.stack }
  process.exitCode = 1
}).finally(async () => {
  if (context) {
    try {
      await context.close()
      result.contextCloseReturned = true
    } catch (error) {
      result.closeError = { name: error.name, message: error.message }
      result.result = 'FAIL'
      process.exitCode = 1
    }
  }
  save()
  process.stdout.write(JSON.stringify({ result: result.result, cases: result.cases.length, pageRequests: result.pageRequests.length, contextCloseReturned: result.contextCloseReturned }) + '\n')
})
