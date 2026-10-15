const fs = require('node:fs')
const path = require('node:path')
const { test, expect } = require(process.env.AGENTEAM_MCP_WEB_PLAYWRIGHT)

test('MCP Owner config, original credential lookup and discovered connection', async ({ page, context }) => {
  const directory = process.env.AGENTEAM_MCP_WEB_PRIVATE
  const input = JSON.parse(fs.readFileSync(path.join(directory, 'input.json'), 'utf8'))
  let stage = 'open'
  try {
    const origin = new URL(input.origin)
    if (origin.protocol !== 'http:' || origin.hostname !== '127.0.0.1' || origin.origin !== input.origin || !origin.port) throw new Error('EXACT_OWNED_ORIGIN_REQUIRED')
    await context.addCookies([{ name: 'agenteam_local_session', value: input.cookie, url: input.origin, httpOnly: true, sameSite: 'Strict' }])
    await page.goto(input.origin + input.route)
    await expect(page.getByRole('heading', { name: 'MCP 连接', exact: true })).toBeVisible()
    await expect(page.getByText('还没有可用服务', { exact: true })).toBeVisible()

    stage = 'config-cancel'
    const add = page.getByRole('button', { name: '添加服务', exact: true })
    await add.focus()
    await page.keyboard.press('Enter')
    let dialog = page.getByRole('dialog', { name: '添加 MCP 服务', exact: true })
    await expect(dialog).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(dialog).toBeHidden()
    await expect(add).toBeFocused()

    stage = 'config-create'
    await add.click()
    dialog = page.getByRole('dialog', { name: '添加 MCP 服务', exact: true })
    stage = 'config-name'
    await expect(dialog).toBeVisible()
    await dialog.getByRole('textbox', { name: '服务名称', exact: true }).fill('Owned browser MCP')
    stage = 'config-endpoint'
    await dialog.getByRole('textbox', { name: 'MCP 地址', exact: true }).fill(input.endpoint)
    stage = 'config-auth-trigger'
    await dialog.getByRole('button', { name: '认证方式', exact: true }).click()
    stage = 'config-auth-option'
    await page.getByRole('option', { name: 'Bearer Token', exact: true }).click()
    stage = 'config-submit'
    const created = page.waitForResponse((r) => r.request().method() === 'POST' && r.url().endsWith('/mcp/configs'))
    await dialog.getByRole('button', { name: '保存配置', exact: true }).click()
    stage = 'config-response'
    const configResponse = await created
    if (configResponse.status() !== 200 || !(await configResponse.json()).config?.id) throw new Error('CONFIG_RECEIPT_FAILED')
    stage = 'config-close'
    await expect(dialog).toBeHidden()
    stage = 'config-detail'
    const detail = page.getByRole('region', { name: '连接详情', exact: true })
    await expect(detail.getByRole('heading', { name: 'Owned browser MCP', exact: true })).toBeVisible()

    stage = 'credential-save'
    await detail.getByRole('button', { name: '设置连接凭据', exact: true }).click()
    dialog = page.getByRole('dialog', { name: '设置连接凭据', exact: true })
    await dialog.getByLabel(/^Token(?: \*)?$/).fill(input.material)
    await dialog.getByRole('button', { name: '保存凭据', exact: true }).click()
    input.material = ''
    await expect(dialog).toBeHidden()
    const unknown = page.getByRole('region', { name: '原凭据操作', exact: true })
    await expect(unknown.getByRole('heading', { name: '凭据保存结果尚未确认', exact: true })).toBeVisible()
    await expect(detail.getByRole('button', { name: '设置连接凭据', exact: true })).toBeDisabled()

    stage = 'credential-lookup'
    const looked = page.waitForResponse((r) => r.request().method() === 'POST' && r.url().endsWith('/mcp/credential-commands/lookup'))
    await unknown.getByRole('button', { name: '查询原凭据操作', exact: true }).click()
    const lookupResponse = await looked
    if (lookupResponse.status() !== 200 || !(await lookupResponse.json()).observed) throw new Error('CREDENTIAL_LOOKUP_FAILED')
    await expect(unknown).toBeHidden()
    await expect(detail.getByRole('button', { name: '使用已保存凭据连接', exact: true })).toBeEnabled()

    stage = 'connect'
    const connected = page.waitForResponse((r) => r.request().method() === 'POST' && r.url().endsWith('/mcp/connections/connect'))
    await detail.getByRole('button', { name: '使用已保存凭据连接', exact: true }).click()
    const connectionResponse = await connected
    const receipt = await connectionResponse.json()
    if (connectionResponse.status() !== 200 || receipt.connection?.state !== 'connected' || receipt.connection?.tools_state !== 'ready' || receipt.connection?.resources_state !== 'ready') throw new Error('CONNECTION_DISCOVERY_FAILED')
    await expect(page.getByRole('region', { name: 'Tool 目录', exact: true }).getByText('Return the supplied text from the owned provider', { exact: true })).toBeVisible()
    await expect(page.getByRole('region', { name: 'Resource 目录', exact: true }).getByText('repo://owned/data', { exact: true })).toBeVisible()
    await page.screenshot({ path: path.join(input.evidence, 'mcp-desktop-light.png'), fullPage: true })

    stage = 'toggle'
    await detail.getByRole('button', { name: '停用连接', exact: true }).click()
    await expect(detail.getByText('连接已停用，目录仅供查看。', { exact: true })).toBeVisible()
    await detail.getByRole('button', { name: '启用连接', exact: true }).click()
    await expect(detail.getByRole('button', { name: '停用连接', exact: true })).toBeEnabled()
    await page.reload()
    await expect(page.getByRole('region', { name: 'Resource 目录', exact: true }).getByText('repo://owned/data', { exact: true })).toBeVisible()

    stage = 'layouts'
    await page.emulateMedia({ colorScheme: 'dark', reducedMotion: 'reduce' })
    await page.setViewportSize({ width: 390, height: 844 })
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
    if (await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)) throw new Error('PAGE_OVERFLOW')
    await page.screenshot({ path: path.join(input.evidence, 'mcp-mobile-dark.png'), fullPage: true })

    stage = 'disconnect'
    await detail.getByRole('button', { name: '断开', exact: true }).click()
    dialog = page.getByRole('dialog', { name: '断开连接', exact: true })
    await dialog.getByRole('button', { name: '断开连接', exact: true }).click()
    await expect(detail.getByRole('button', { name: '设置连接凭据', exact: true })).toBeEnabled()
    stage = 'delete-config'
    await detail.getByRole('button', { name: '删除配置', exact: true }).click()
    dialog = page.getByRole('dialog', { name: '删除配置', exact: true })
    await dialog.getByRole('button', { name: '删除配置', exact: true }).click()
    await expect(page.getByText('还没有可用服务', { exact: true })).toBeVisible()
    stage = 'complete'
    fs.writeFileSync(path.join(directory, 'complete.json'), JSON.stringify({ complete: true }), { mode: 0o600 })
  } catch (error) {
    const kind = error?.name === 'TimeoutError' ? 'TIMEOUT' : /^[A-Z_]{1,64}$/.test(error?.message || '') ? error.message : 'ASSERTION_FAILED'
    fs.writeFileSync(path.join(directory, 'failure.json'), JSON.stringify({ stage, error: kind }), { mode: 0o600 })
    // No trace, request dump, or filled credential form is exported.
    throw new Error('MCP_BROWSER_FAILED')
  } finally {
    input.material = ''
    input.cookie = ''
  }
})
