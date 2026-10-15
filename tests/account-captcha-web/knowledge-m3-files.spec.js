import { test, expect } from '@playwright/test'
import { readFileSync, writeFileSync, statSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { createRequire } from 'node:module'
import { createHash } from 'node:crypto'

const require = createRequire(import.meta.url)
const { PNG } = require(join(dirname(require.resolve('playwright-core/package.json')), 'lib/utilsBundle.js'))
const root = process.env.AGENTEAM_AUTH_WEB_PRIVATE
const digest = (data) => createHash('sha256').update(data).digest('hex')
async function press(page, name) {
  const button = page.getByRole('button', { name, exact: true })
  await expect(button).toBeEnabled()
  await button.focus()
  await expect(button).toBeFocused()
  await page.keyboard.press('Enter')
}
async function currentText(page, expected) {
  await expect(async () => {
    const content = page.locator('pre.canonical-text')
    const actual = await content.count() ? await content.textContent({ timeout: 500 }).catch(() => null) : null
    const status = page.locator('.document-heading .ui-badge')
    const ready = await status.count() ? await status.textContent({ timeout: 500 }).catch(() => null) : null
    if (actual !== expected || ready !== '就绪') {
      const retry = page.getByRole('button', { name: '从开头重读', exact: true })
      if (await retry.isEnabled()) await retry.click()
    }
    await expect(page.locator('pre.canonical-text')).toHaveText(expected, { timeout: 500 })
    await expect(status).toHaveText('就绪', { timeout: 500 })
  }).toPass({ timeout: 10000, intervals: [100, 200, 500] })
}
// This pixel gate is specific to the public, white-page PDF fixture. A loaded
// native document may report complete before Chromium paints its first page.
async function waitForPublicPDFPaint(page, frame) {
  const bounds = await frame.boundingBox()
  const deadline = Date.now() + 2500
  let captures = 0
  do {
    const png = PNG.sync.read(await page.screenshot({ animations: 'disabled' }))
    let white = 0
    let samples = 0
    const right = Math.min(png.width, Math.floor(bounds.x + bounds.width - 8))
    const bottom = Math.min(png.height, Math.floor(bounds.y + bounds.height - 8))
    for (let y = Math.max(0, Math.ceil(bounds.y + 60)); y < bottom; y += 3) {
      for (let x = Math.max(0, Math.ceil(bounds.x + 8)); x < right; x += 3) {
        const n = (y * png.width + x) * 4
        samples++
        if (png.data[n] > 235 && png.data[n + 1] > 235 && png.data[n + 2] > 235) white++
      }
    }
    captures++
    if (samples > 1000 && white / samples > 0.2) return captures
  } while (Date.now() < deadline)
  throw new Error('PUBLIC_PDF_FIRST_PAINT_NOT_OBSERVED')
}
async function currentDocument(page, response, operation, expected) {
  const address = new URL(response.url())
  expect(address.origin).toBe(process.env.AGENTEAM_AUTH_WEB_ORIGIN)
  const match = /^\/api\/v1\/projects\/([a-f0-9-]{36})\/knowledge\/documents\/([a-f0-9-]{36})\/(create-file|replace-file)$/.exec(address.pathname)
  expect(match).not.toBeNull()
  expect(match[3]).toBe(operation)
  const target = match[2]
  const information = page.locator('details.document-information')
  if (!(await information.evaluate((element) => element.open))) await information.locator('summary').click()
  const link = information.getByRole('link', { name: '文档链接', exact: true })
  await expect(link).toBeVisible()
  await expect(link).toHaveAttribute('href', new RegExp(`/knowledge/${target}$`))
  await information.locator('summary').click()
  // Read the accepted document through the original Owner boundary. Chromium
  // need not retain a second DevTools copy of the app-consumed POST body.
  const read = await page.context().request.get(address.href.replace(/\/(create-file|replace-file)$/, ''), { maxRedirects: 0, timeout: 5000 })
  try {
    expect(read.status()).toBe(200)
    const raw = await read.body()
    expect(raw.length).toBeLessThanOrEqual(2097152)
    const document = JSON.parse(raw.toString('utf8')).active
    expect(document.id).toBe(target)
    expect(document.project_id).toBe(match[1])
    expect(document.title).toBe(expected.title)
    expect(document.media_type).toBe(expected.media)
    expect(document.content_version).toBe(expected.version)
    return document
  } finally { await read.dispose() }
}

test('Owner uploads four file formats, replaces PDF, reads and downloads current sources', async ({ page }) => {
  const path = join(root, 'knowledge-files-material.json')
  if (!statSync(path).isFile() || statSync(path).size > 16384) throw new Error('BOUNDED_KNOWLEDGE_FILES_MATERIAL_REQUIRED')
  const material = JSON.parse(readFileSync(path, 'utf8'))
  if (!Array.isArray(material.files) || material.files.length !== 4 || !/^[a-f0-9]{64}$/.test(material.input_hash)) throw new Error('FIXED_KNOWLEDGE_FILES_MATERIAL_REQUIRED')
  const publicResult = { input_hash: material.input_hash, stage: 'starting', complete: false, documents: [], downloads: [], screenshots: [], errors: 0, receipt_status: 0, receipt_bytes: 0, receipt_json: false }
  const save = () => writeFileSync(join(root, 'knowledge-files-result.json'), JSON.stringify(publicResult), { mode: 0o600 })
  const stage = (name) => { publicResult.stage = name; save() }
  const writes = []
  page.on('request', (request) => {
    if (request.method() === 'POST' && /\/(create-file|replace-file)$/.test(new URL(request.url()).pathname)) writes.push(request)
  })
  page.on('pageerror', () => { publicResult.errors++; save() })
  await page.setViewportSize({ width: 1440, height: 960 })
  await page.emulateMedia({ colorScheme: 'light', reducedMotion: 'reduce' })
  await page.context().addCookies([{ name: 'agenteam_local_session', value: material.cookie, url: process.env.AGENTEAM_AUTH_WEB_ORIGIN, httpOnly: true, sameSite: 'Lax', secure: false }])
  try {
    await page.goto(material.route)
    await expect(page.getByRole('region', { name: '项目知识库', exact: true })).toBeVisible()
    for (const [ordinal, source] of material.files.entries()) {
      stage(`upload-${ordinal}-open`)
      await press(page, '上传文档')
      const dialog = page.getByRole('dialog', { name: '上传文档', exact: true })
      const title = dialog.getByRole('textbox', { name: '标题', exact: true })
      stage(`upload-${ordinal}-focus`)
      await expect(title).toBeFocused()
      if (ordinal === 0) {
        stage(`upload-${ordinal}-oversize`)
        await dialog.locator('input[type=file]').setInputFiles({ name: 'oversize.pdf', mimeType: 'application/pdf', buffer: Buffer.alloc(1048577) })
        await expect(dialog.getByRole('status')).toContainText('1 MiB')
        await expect(dialog.getByRole('button', { name: '保存文件', exact: true })).toBeDisabled()
      }
      stage(`upload-${ordinal}-choose`)
      await title.fill(source.title)
      await dialog.locator('input[type=file]').setInputFiles({ name: source.filename, mimeType: source.media, buffer: Buffer.from(source.bytes_base64, 'base64') })
      const sent = page.waitForResponse((response) => response.request().method() === 'POST' && response.url().endsWith('/create-file'))
      stage(`upload-${ordinal}-submit`)
      await press(page, '保存文件')
      const response = await sent
      stage(`upload-${ordinal}-status`)
      publicResult.receipt_status = response.status()
      publicResult.receipt_bytes = 0
      publicResult.receipt_json = false
      expect(response.status()).toBe(200)
      stage(`upload-${ordinal}-selected`)
      await expect(dialog).toHaveCount(0)
      await expect(page.locator('.knowledge-document h2')).toHaveText(source.title)
      stage(`upload-${ordinal}-fields`)
      const current = await currentDocument(page, response, 'create-file', { title: source.title, media: source.media, version: '1' })
      expect(response.request().postDataJSON()).toEqual({ parent_document_id: null, title: source.title, media_type: source.media, bytes_base64: source.bytes_base64 })
      expect(writes).toHaveLength(ordinal + 1)
      publicResult.documents.push({ id: current.id, version: '1', media: source.media })
      stage(`upload-${ordinal}-projection`)
      await currentText(page, source.text)
      stage(`download-${ordinal}`)
      const waiting = page.waitForEvent('download')
      await press(page, '下载原文档')
      const download = await waiting
      const destination = join(root, `knowledge-download-${ordinal}.bin`)
      await download.saveAs(destination)
      expect(digest(readFileSync(destination))).toBe(digest(Buffer.from(source.bytes_base64, 'base64')))
      publicResult.downloads.push({ ordinal, sha256: digest(readFileSync(destination)) })
      if (source.media === 'application/pdf') {
        stage('pdf-preview')
        await press(page, '预览 PDF')
        const frame = page.getByTitle('当前 PDF 文档预览', { exact: true })
        await expect(frame).toBeVisible()
        await expect(frame).toHaveAttribute('data', /^blob:/)
        await expect(frame).toHaveAttribute('type', 'application/pdf')
        // Chromium's native PDF viewer need not expose an embed node. Check
        // the loaded PDF document and retain actual rendered pixels for review.
        await expect.poll(() => frame.evaluate((element) => ({
          type: element.contentDocument?.contentType,
          ready: element.contentDocument?.readyState,
        }))).toEqual({ type: 'application/pdf', ready: 'complete' })
        await waitForPublicPDFPaint(page, frame)
        await page.screenshot({ path: join(root, 'knowledge-files-1440-light.png'), animations: 'disabled' })
        publicResult.screenshots.push('knowledge-files-1440-light.png')
        stage('pdf-replace')
        await press(page, '替换原文件')
        const replacement = page.getByRole('dialog', { name: '替换原文件', exact: true })
        await replacement.locator('input[type=file]').setInputFiles({ name: source.filename, mimeType: source.media, buffer: Buffer.from(material.updated_pdf.bytes_base64, 'base64') })
        const written = page.waitForResponse((reply) => reply.request().method() === 'POST' && reply.url().endsWith('/replace-file'))
        await press(page, '保存文件')
        const reply = await written
        expect(reply.status()).toBe(200)
        await expect(replacement).toHaveCount(0)
        await expect(frame).toHaveCount(0)
        const current = await currentDocument(page, reply, 'replace-file', { title: source.title, media: source.media, version: '2' })
        expect(current.id).toBe(publicResult.documents[ordinal].id)
        expect(reply.request().postDataJSON()).toEqual({ expected_version: '1', media_type: source.media, bytes_base64: material.updated_pdf.bytes_base64 })
        publicResult.documents[ordinal].version = '2'
        await currentText(page, material.updated_pdf.text)
      }
    }
    stage('narrow-dark')
    await page.setViewportSize({ width: 390, height: 844 })
    await page.emulateMedia({ colorScheme: 'dark', reducedMotion: 'reduce' })
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
    await press(page, '文档树')
    await expect(page.getByRole('dialog', { name: '文档树', exact: true })).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(page.getByRole('dialog', { name: '文档树', exact: true })).toHaveCount(0)
    await expect(page.getByRole('button', { name: '文档树', exact: true })).toBeFocused()
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
    await page.screenshot({ path: join(root, 'knowledge-files-390-dark.png'), animations: 'disabled' })
    publicResult.screenshots.push('knowledge-files-390-dark.png')
    expect(writes).toHaveLength(5)
    expect(new Set(writes.map((request) => request.url())).size).toBe(5)
    const keys = writes.map((request) => request.headers()['idempotency-key'])
    expect(keys.every((key) => typeof key === 'string' && key.length > 0)).toBe(true)
    expect(new Set(keys).size).toBe(5)
    expect(publicResult.errors).toBe(0)
    publicResult.complete = true
    stage('complete')
  } catch (error) {
    // These fixture inputs are public sentences, and credentials never enter
    // this page's DOM. Keep bounded observable state, not an exception string.
    publicResult.failure = await page.evaluate(() => {
      const active = document.activeElement
      const dialogs = [...document.querySelectorAll('[role=dialog]')].filter((element) => element.getClientRects().length)
      const dialog = dialogs[0]
      const input = dialog?.querySelector('[data-autofocus]')
      return {
        visible_dialogs: dialogs.length,
        autofocus_exists: !!input,
        autofocus_active: input === active,
        autofocus_inert: !!input?.closest('[inert]'),
        active_tag: ['INPUT', 'BUTTON', 'SECTION', 'BODY', 'IFRAME'].includes(active?.tagName) ? active.tagName : 'OTHER',
      }
    }).catch(() => null)
    if (publicResult.failure) publicResult.failure.title_matches = await page.getByRole('dialog', { name: '上传文档', exact: true }).getByRole('textbox', { name: '标题', exact: true }).count()
    if (await page.screenshot({ path: join(root, 'knowledge-files-failure.png'), animations: 'disabled', timeout: 1000 }).then(() => true).catch(() => false)) {
      publicResult.screenshots.push('knowledge-files-failure.png')
    }
    throw error
  } finally { save() }
})
