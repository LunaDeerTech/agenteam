import { test as base, expect, type Page } from '@playwright/test'
import { createHash, randomUUID } from 'node:crypto'
import { copyFile, mkdir, mkdtemp, readFile, realpath, rm, writeFile } from 'node:fs/promises'
import { createRequire } from 'node:module'
import { dirname, join } from 'node:path'
import { pathToFileURL } from 'node:url'

const closure = [
  'src/components/ui/UiDialog.vue',
  'src/components/ui/UiDrawer.vue',
  'src/components/ui/UiButton.vue',
  'src/components/ui/UiIcon.vue',
  'src/components/ui/UiSpinner.vue',
  'src/components/ui/types.ts',
  'src/composables/useLayer.ts',
  'src/styles/tokens.css',
  'src/styles/base.css',
  'src/styles/components.css',
]

const shell = `<script setup lang="ts">
import { ref } from 'vue'
import UiDialog from './src/components/ui/UiDialog.vue'
import UiDrawer from './src/components/ui/UiDrawer.vue'
const query = new URLSearchParams(location.search)
const Surface = query.get('kind') === 'drawer' ? UiDrawer : UiDialog
const outside = query.get('outside') !== 'false'
const escape = query.get('escape') !== 'false'
const open = ref(false), nested = ref(false), clicks = ref(0)
const events = ref<string[]>([])
</script>
<template>
  <main style="padding:24px">
    <button data-testid="launch" @click="open = true">Open surface</button>
    <label>Later input <input data-testid="later-input" /></label>
    <output data-testid="events">{{ JSON.stringify(events) }}</output>
    <Surface v-model:open="open" title="Focus surface" :close-on-outside="outside" :close-on-escape="escape"
      @update:open="value => events.push('update:' + value)" @close="reason => events.push('close:' + reason)">
      <label>Inside input <input data-testid="inside-input" data-autofocus /></label>
      <button data-testid="inside-button" @click="clicks++">Clicked {{ clicks }}</button>
      <a data-testid="inside-link" href="#inside">Inside link</a>
      <p data-testid="inside-text">Selectable content</p>
      <button data-testid="nested-launch" @click="nested = true">Open nested</button>
      <UiDialog v-model:open="nested" title="Nested surface"
        @close="reason => events.push('nested:' + reason)">
        <input data-testid="nested-input" data-autofocus />
      </UiDialog>
      <template #footer="{ close }"><button data-testid="footer-close" @click="close">Done</button></template>
    </Surface>
  </main>
</template>`

type Harness = { origin: string; nonce: string }
const test = base.extend<{ ownOrigin: void }, { harness: Harness }>({
  harness: [
    async ({}, use, workerInfo) => {
      const webRoot = process.env.AGENTEAM_DIALOG_WEB_ROOT!
      const runDir = process.env.AGENTEAM_DIALOG_RUN_DIR!
      const webRequire = createRequire(join(webRoot, 'package.json'))
      const { createServer } = await import(pathToFileURL(webRequire.resolve('vite')).href)
      const { default: vue } = await import(
        pathToFileURL(webRequire.resolve('@vitejs/plugin-vue')).href
      )
      await mkdir(runDir, { recursive: true })
      const root = await mkdtemp(join(runDir, 'component-'))
      const nonce = randomUUID()
      const inputs: Record<string, string> = {}
      for (const path of closure) {
        await mkdir(dirname(join(root, path)), { recursive: true })
        await copyFile(join(webRoot, path), join(root, path))
        inputs[path] = createHash('sha256')
          .update(await readFile(join(root, path)))
          .digest('hex')
      }
      await writeFile(join(root, 'Harness.vue'), shell)
      await writeFile(
        join(root, 'main.ts'),
        `import { createApp } from 'vue'; import Harness from './Harness.vue';
import './src/styles/tokens.css'; import './src/styles/base.css'; import './src/styles/components.css';
document.body.style.overflow = 'auto'; createApp(Harness).mount('#app');`,
      )
      await writeFile(
        join(root, 'index.html'),
        `<!doctype html><html><head><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="harness-nonce" content="${nonce}"></head><body><div id="app"></div><script type="module" src="/main.ts"></script></body></html>`,
      )
      const server = await createServer({
        configFile: false,
        root,
        publicDir: false,
        cacheDir: join(root, '.vite'),
        clearScreen: false,
        logLevel: 'error',
        plugins: [vue()],
        resolve: {
          alias: { vue: webRequire.resolve('vue/dist/vue.esm-bundler.js') },
          dedupe: ['vue'],
        },
        server: {
          host: '127.0.0.1',
          port: 0,
          strictPort: true,
          hmr: false,
          cors: false,
          fs: { allow: [root, await realpath(join(webRoot, 'node_modules'))] },
        },
      })
      const lifecycle = join(runDir, `server-${workerInfo.workerIndex}.json`)
      let origin = ''
      try {
        await server.listen()
        const address = server.httpServer!.address()
        if (!address || typeof address === 'string') throw new Error('LOOPBACK_ADDRESS_REQUIRED')
        origin = `http://127.0.0.1:${address.port}`
        await writeFile(
          lifecycle,
          JSON.stringify({ pid: process.pid, origin, nonce, inputs, closed: false }, null, 2),
        )
        await use({ origin, nonce })
      } finally {
        await server.close()
        await writeFile(
          lifecycle,
          JSON.stringify(
            { pid: process.pid, origin, nonce, inputs, closed: !server.httpServer?.listening },
            null,
            2,
          ),
        )
        await rm(root, { recursive: true, force: true })
      }
    },
    { scope: 'worker' },
  ],
  ownOrigin: [
    async ({ context, page, harness }, use, testInfo) => {
      const blocked: string[] = []
      await context.route('**/*', async (route) => {
        if (new URL(route.request().url()).origin === harness.origin) await route.continue()
        else {
          blocked.push(route.request().url())
          await route.abort('blockedbyclient')
        }
      })
      await page.addInitScript(() => {
        const events: object[] = []
        Object.assign(window, { pointerEvidence: events })
        const label = (node: EventTarget | null) =>
          node instanceof Element
            ? node.getAttribute('data-testid') || node.id || node.className || node.tagName
            : null
        for (const type of [
          'pointerdown',
          'mousedown',
          'mouseup',
          'click',
          'focusin',
          'focusout',
        ]) {
          for (const capture of [true, false]) {
            document.addEventListener(
              type,
              (event) => {
                if (events.length < 1000)
                  events.push({
                    type,
                    capture,
                    target: label(event.target),
                    active: label(document.activeElement),
                    defaultPrevented: event.defaultPrevented,
                  })
              },
              capture,
            )
          }
        }
      })
      try {
        await use()
      } finally {
        if (!page.isClosed()) {
          await testInfo.attach('pointer-events', {
            body: Buffer.from(
              JSON.stringify(
                {
                  blocked,
                  events: await page.evaluate(
                    () => (window as unknown as { pointerEvidence: object[] }).pointerEvidence,
                  ),
                },
                null,
                2,
              ),
            ),
            contentType: 'application/json',
          })
        }
        expect(blocked, 'the component harness must not request other origins').toEqual([])
      }
    },
    { auto: true },
  ],
})

async function visit(page: Page, harness: Harness, kind: string, options = '') {
  await page.goto(`${harness.origin}/?kind=${kind}&nonce=${harness.nonce}${options}`)
  await expect(page.locator('meta[name="harness-nonce"]')).toHaveAttribute('content', harness.nonce)
  await expect(page.getByTestId('launch')).toBeVisible()
}

async function open(page: Page) {
  await page.getByTestId('launch').click()
  await expect(page.getByRole('dialog', { name: 'Focus surface', exact: true })).toBeVisible()
  await expect(page.getByTestId('inside-input')).toBeFocused()
  await expect(page.locator('#app')).toHaveJSProperty('inert', true)
  await expect(page.locator('body')).toHaveCSS('overflow', 'hidden')
}

async function clickOverlay(page: Page) {
  const box = await page.locator('.ui-overlay').last().boundingBox()
  if (!box) throw new Error('OVERLAY_BOX_REQUIRED')
  const point = { x: box.x + 2, y: box.y + 2 }
  expect(
    await page.evaluate(
      ({ x, y }) =>
        document.elementFromPoint(x, y) === [...document.querySelectorAll('.ui-overlay')].at(-1),
      point,
    ),
    'coordinate must hit the actual top overlay',
  ).toBe(true)
  await page.mouse.click(point.x, point.y)
}

async function restored(page: Page) {
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await expect(page.getByTestId('launch')).toBeFocused()
  await expect(page.locator('#app')).toHaveJSProperty('inert', false)
  await expect(page.locator('body')).toHaveCSS('overflow', 'auto')
}

for (const kind of ['dialog', 'drawer']) {
  for (const width of [390, 1440]) {
    for (const reducedMotion of ['no-preference', 'reduce'] as const) {
      test(`${kind} ${width} ${reducedMotion}: outside keeps trigger focus`, async ({
        page,
        harness,
      }) => {
        await page.setViewportSize({ width, height: 900 })
        await page.emulateMedia({ reducedMotion })
        await visit(page, harness, kind)
        await open(page)
        await page.getByTestId('inside-input').click()
        await page.getByTestId('inside-input').fill('Editable inside')
        await expect(page.getByTestId('inside-input')).toHaveValue('Editable inside')
        await clickOverlay(page)
        await restored(page)
        await expect(page.getByTestId('events')).toHaveText('["update:false","close:outside"]')
        await page.getByTestId('later-input').click()
        await page.getByTestId('later-input').fill('User chose this input')
        // Observe beyond the accepted leave transition, so a late focus repair cannot pass.
        await page.waitForTimeout(250)
        await expect(page.getByTestId('later-input')).toBeFocused()
        for (const action of ['escape', 'button', 'footer']) {
          await open(page)
          if (action === 'escape') await page.keyboard.press('Escape')
          else if (action === 'button')
            await page.getByRole('button', { name: '关闭', exact: true }).click()
          else await page.getByTestId('footer-close').click()
          await restored(page)
        }
        await expect(page.getByTestId('events')).toHaveText(
          '["update:false","close:outside","update:false","close:escape","update:false","close:action","update:false","close:action"]',
        )
      })
    }
  }

  test(`${kind}: disabled outside and Escape preserve editing and action`, async ({
    page,
    harness,
  }) => {
    await page.setViewportSize({ width: 390, height: 900 })
    await visit(page, harness, kind, '&outside=false&escape=false')
    await open(page)
    await clickOverlay(page)
    await page.keyboard.press('Escape')
    await expect(page.getByRole('dialog')).toHaveCount(1)
    await expect(page.getByTestId('events')).toHaveText('[]')
    await page.getByTestId('inside-input').click()
    await page.getByTestId('inside-input').fill('Still editable')
    await expect(page.getByTestId('inside-input')).toBeFocused()
    await expect(page.getByTestId('inside-input')).toHaveValue('Still editable')
    await page.getByTestId('inside-button').click()
    await expect(page.getByTestId('inside-button')).toHaveText('Clicked 1')
    await page.getByTestId('inside-link').click()
    await expect(page).toHaveURL(/#inside$/)
    await expect(page.getByTestId('inside-link')).toBeFocused()
    await expect(page.getByTestId('events')).toHaveText('[]')
    await page.getByTestId('footer-close').click()
    await restored(page)
    await expect(page.getByTestId('events')).toHaveText('["update:false","close:action"]')
  })
}

test('two modal layers close only the top and restore the lower trigger', async ({
  page,
  harness,
}) => {
  await page.setViewportSize({ width: 390, height: 900 })
  await visit(page, harness, 'dialog')
  await open(page)
  await page.getByTestId('nested-launch').click()
  await expect(page.getByTestId('nested-input')).toBeFocused()
  await expect(page.locator('[role="dialog"]')).toHaveCount(2)
  await expect(page.locator('[role="dialog"]').first()).toHaveJSProperty('inert', true)
  await clickOverlay(page)
  await expect(page.locator('[role="dialog"]')).toHaveCount(1)
  await expect(page.getByTestId('nested-launch')).toBeFocused()
  await expect(page.locator('#app')).toHaveJSProperty('inert', true)
  await expect(page.getByTestId('events')).toHaveText('["nested:outside"]')
  await page.keyboard.press('Escape')
  await restored(page)
  await expect(page.getByTestId('events')).toHaveText(
    '["nested:outside","update:false","close:escape"]',
  )
})
