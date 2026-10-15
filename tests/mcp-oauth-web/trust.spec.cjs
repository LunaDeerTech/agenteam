const fs = require('node:fs')
const path = require('node:path')
const { test, expect } = require(process.env.AGENTEAM_MCP_WEB_PLAYWRIGHT)

test('private browser trust accepts only the owned app root', async ({ page }) => {
  const directory = process.env.AGENTEAM_MCP_WEB_PRIVATE
  const input = JSON.parse(fs.readFileSync(path.join(directory, 'input.json'), 'utf8'))
  let stage = 'inputs'
  try {
    for (const value of [input.trusted, input.untrusted]) {
      const url = new URL(value)
      if (url.protocol !== 'https:' || url.hostname !== '127.0.0.1' || !url.port || url.pathname !== '/trust-check' || url.search || url.hash || url.username || url.password) throw new Error()
    }
    stage = 'trusted-ca'
    const response = await page.goto(input.trusted)
    expect(response.status()).toBe(200)
    await expect(page.locator('body')).toHaveText('owned-browser-tls-ok')
    stage = 'untrusted-ca'
    await expect(page.goto(input.untrusted)).rejects.toThrow(/ERR_CERT_AUTHORITY_INVALID/)
    fs.writeFileSync(path.join(directory, 'complete.json'), JSON.stringify({ complete: true }), { mode: 0o600 })
  } catch (error) {
    const message = typeof error?.message === 'string' ? error.message : ''
    const code = ['ERR_CERT_AUTHORITY_INVALID', 'ERR_CERT_INVALID', 'ERR_CERT_COMMON_NAME_INVALID',
      'ERR_CERT_DATE_INVALID', 'ERR_SSL_VERSION_OR_CIPHER_MISMATCH', 'ERR_SSL_PROTOCOL_ERROR',
      'ERR_CONNECTION_REFUSED', 'ERR_CONNECTION_RESET'].find(value => message.includes(value))
    fs.writeFileSync(path.join(directory, 'failure.json'), JSON.stringify({ stage, error: code || 'MCP_BROWSER_TRUST_FAILED' }), { mode: 0o600 })
    throw new Error('MCP_BROWSER_TRUST_FAILED')
  }
})
