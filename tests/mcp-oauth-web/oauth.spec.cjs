const fs = require('node:fs')
const path = require('node:path')
const https = require('node:https')
const net = require('node:net')
const { test, expect } = require(process.env.AGENTEAM_MCP_WEB_PLAYWRIGHT)

// This relay validates the real issuer's TLS using the fixture CA. It does
// not prove Chromium's independent trust-store configuration. Only the exact
// issuer authorization GET is relayed; App Complete/lookup remain untouched.
function authorize(input, url) {
  return new Promise((resolve, reject) => {
    const request = https.request({ hostname: input.issuerIP, port: 8443,
      servername: 'mcp.fixture.test', ca: fs.readFileSync(input.caFile),
      rejectUnauthorized: true, method: 'GET', path: url.pathname + url.search,
      headers: { Host: 'mcp.fixture.test:8443' }, timeout: 5000 }, response => {
      let size = 0
      response.on('data', bytes => { size += bytes.length; if (size > 4096) response.destroy() })
      response.on('error', () => reject(new Error('ISSUER_RESPONSE_FAILED')))
      response.on('end', () => {
        try {
          const target = new URL(response.headers.location)
          if (response.statusCode !== 302 || size > 4096 || target.origin !== input.origin || target.pathname !== '/mcp/oauth/callback' || target.username || target.password || target.hash || !target.searchParams.get('code') || target.searchParams.get('state') !== url.searchParams.get('state')) throw new Error()
          resolve(target.href)
        } catch { reject(new Error('ISSUER_REDIRECT_FAILED')) }
      })
    })
    request.on('timeout', () => request.destroy())
    request.on('error', () => reject(new Error('ISSUER_TLS_FAILED')))
    request.end()
  })
}

test('OAuth Owner journey and original lookup with a real issuer TLS relay', async ({ page, context }) => {
  const directory = process.env.AGENTEAM_MCP_WEB_PRIVATE
  const input = JSON.parse(fs.readFileSync(path.join(directory, 'input.json'), 'utf8'))
  let stage = 'inputs', relays = 0, completes = 0, lookups = 0, callbackClean = true
  let originalKey = '', lookupKey = ''
  let beginStatus = 0, completeStatus = 0, lookupStatus = 0, relayFailure = 'none'
  let beginOAuthPresent = false, beginConnectionState = 'unobserved', beginErrorCode = 'none'
  let beginBodyObservation = 'unobserved'
  try {
    const origin = new URL(input.origin)
    if (origin.protocol !== 'https:' || origin.hostname !== '127.0.0.1' || origin.origin !== input.origin || !origin.port || net.isIP(input.issuerIP) !== 4 || !path.isAbsolute(input.caFile) || input.endpoint !== 'https://mcp.fixture.test:8443/oauth/mcp') throw new Error('EXACT_OWNED_INPUT_REQUIRED')
    await context.route('https://mcp.fixture.test:8443/oauth/authorize?*', async route => {
      const request = route.request(), url = new URL(request.url())
      if (request.method() !== 'GET' || url.origin !== 'https://mcp.fixture.test:8443' || url.pathname !== '/oauth/authorize' || url.hash || url.username || url.password) throw new Error('ISSUER_ROUTE_REFUSED')
      let redirect
      try {
        redirect = await authorize(input, url)
      } catch (error) {
        relayFailure = ['ISSUER_RESPONSE_FAILED', 'ISSUER_REDIRECT_FAILED', 'ISSUER_TLS_FAILED'].includes(error?.message)
          ? error.message : 'ISSUER_ROUTE_FAILED'
        throw error
      }
      relays++
      await route.fulfill({ status: 302, headers: { location: redirect, 'cache-control': 'no-store' }, body: '' })
    })
    page.on('request', request => {
      const url = new URL(request.url())
      if (url.origin !== input.origin || request.method() !== 'POST') return
      if (url.pathname === '/api/v1/mcp/oauth/complete') {
        completes++
        originalKey = request.headers()['idempotency-key'] || ''
        const callback = new URL(page.url())
        callbackClean &&= callback.pathname === '/mcp/oauth/callback' && !callback.search && !callback.hash
      }
      if (url.pathname.endsWith('/mcp/commands/lookup')) {
        lookups++
        lookupKey = request.headers()['idempotency-key'] || ''
      }
    })
    page.on('response', response => {
      const url = new URL(response.url())
      if (url.origin !== input.origin || response.request().method() !== 'POST') return
      if (url.pathname.endsWith('/mcp/oauth/begin')) beginStatus = response.status()
      if (url.pathname === '/api/v1/mcp/oauth/complete') completeStatus = response.status()
      if (url.pathname.endsWith('/mcp/commands/lookup')) lookupStatus = response.status()
    })
    await context.addCookies([{ name: '__Host-agenteam_session', value: input.cookie, url: input.origin, secure: true, httpOnly: true, sameSite: 'Strict' }])
    stage = 'open'
    await page.goto(input.origin + input.route)
    await expect(page.getByRole('heading', { name: 'MCP 连接', exact: true })).toBeVisible()
    stage = 'config-create'
    await page.getByRole('button', { name: '添加服务', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: '添加 MCP 服务', exact: true })
    await dialog.getByRole('textbox', { name: '服务名称', exact: true }).fill('Owned OAuth browser MCP')
    await dialog.getByRole('textbox', { name: 'MCP 地址', exact: true }).fill(input.endpoint)
    await dialog.getByRole('button', { name: '认证方式', exact: true }).click()
    await page.getByRole('option', { name: 'OAuth 2.1 授权', exact: true }).click()
    const saved = page.waitForResponse(r => r.request().method() === 'POST' && new URL(r.url()).pathname.endsWith('/mcp/configs'))
    await dialog.getByRole('button', { name: '保存配置', exact: true }).click()
    if ((await saved).status() !== 200) throw new Error('CONFIG_RECEIPT_FAILED')
    await expect(dialog).toBeHidden()
    stage = 'begin'
    const begun = page.waitForResponse(r => r.request().method() === 'POST' && new URL(r.url()).pathname.endsWith('/mcp/oauth/begin'))
    await page.getByRole('region', { name: '连接详情', exact: true }).getByRole('button', { name: '授权并连接', exact: true }).click()
    const begunResponse = await begun
    stage = 'begin-authorization'
    if (begunResponse.status() !== 200) throw new Error('BEGIN_HTTP_FAILED')
    let begunBytes
    try {
      // The real SPA may already have navigated to the issuer/callback. A
      // detached document's response body is diagnostic only; the actual
      // callback, single exchange and original lookup below prove success.
      begunBytes = await begunResponse.body()
      if (begunBytes.length > 32768) throw new Error()
      const value = JSON.parse(begunBytes.toString('utf8'))
      beginBodyObservation = 'decoded'
      beginOAuthPresent = !!value.oauth && typeof value.oauth === 'object'
      const state = value.connection?.state, code = value.connection?.last_error_code
      beginConnectionState = ['unconnected', 'connecting', 'connected', 'auth_required', 'error', 'needs_reconnect'].includes(state) ? state : 'invalid'
      beginErrorCode = code === undefined ? 'none' : typeof code === 'string' && /^mcp_[a-z0-9_]{1,64}$/.test(code) ? code : 'invalid'
    } catch {
      beginBodyObservation = begunBytes === undefined ? 'unavailable' : 'invalid'
    } finally { begunBytes?.fill(0) }
    stage = 'callback-heading'
    await expect(page.getByRole('heading', { name: '连接 MCP 服务', exact: true })).toBeVisible()
    stage = 'callback-unknown-state'
    await expect(page.getByRole('alert')).toContainText('授权结果尚未确认')
    stage = 'callback-original-metadata'
    await expect(page.getByRole('button', { name: '查询原操作', exact: true })).toBeVisible()
    if (!callbackClean || relays !== 1 || completes !== 1 || !originalKey) throw new Error('CALLBACK_NOT_SINGLE_CLEAN_POST')
    const stored = await page.evaluate(() => sessionStorage.getItem('agenteam.mcp.oauth.v1'))
    if (!stored || JSON.parse(stored).command !== 'oauth.complete' || Object.hasOwn(JSON.parse(stored), 'code')) throw new Error('ORIGINAL_METADATA_MISSING')
    stage = 'reload-original-lookup'
    await page.reload()
    await expect(page.getByRole('status')).toContainText('授权已完成')
    if (completes !== 1 || lookups !== 1 || lookupKey !== originalKey) throw new Error('ORIGINAL_LOOKUP_REQUIRED')
    if (await page.evaluate(() => sessionStorage.getItem('agenteam.mcp.oauth.v1')) !== null) throw new Error('COMPLETED_METADATA_RETAINED')
    stage = 'connection-directory'
    await page.getByRole('button', { name: '返回 MCP 设置', exact: true }).click()
    await expect(page.getByRole('heading', { name: 'MCP 连接', exact: true })).toBeVisible()
    await expect(page.getByRole('region', { name: 'Tool 目录', exact: true }).getByText('Return the supplied text from the owned provider', { exact: true })).toBeVisible()
    await expect(page.getByRole('region', { name: 'Resource 目录', exact: true }).getByText('repo://owned/data', { exact: true })).toBeVisible()
    await page.screenshot({ path: path.join(input.evidence, 'oauth-connected.png') })
    fs.writeFileSync(path.join(directory, 'complete.json'), JSON.stringify({ complete: true, issuerTLSRelay: true, browserTrustStoreNotVerified: true, relays, completes, lookups }), { mode: 0o600 })
  } catch {
    // Never emit the provider's authorization query or callback bytes.
    fs.writeFileSync(path.join(directory, 'failure.json'), JSON.stringify({ stage, error: 'MCP_OAUTH_BROWSER_FAILED' }), { mode: 0o600 })
    let location = 'unavailable'
    try {
      const current = new URL(page.url())
      location = current.origin === input.origin
        ? current.pathname === '/mcp/oauth/callback' ? 'callback' : 'app-other'
        : current.origin === 'https://mcp.fixture.test:8443' ? 'issuer' : 'other'
    } catch { /* Keep only the closed location category. */ }
    fs.writeFileSync(path.join(input.evidence, 'oauth-failure.json'), JSON.stringify({
      stage, location, relays, relayFailure, completes, lookups, beginStatus, completeStatus, lookupStatus, callbackClean,
      beginBodyObservation, beginOAuthPresent, beginConnectionState, beginErrorCode,
    }), { mode: 0o600 })
    throw new Error('MCP_OAUTH_BROWSER_FAILED')
  }
})
