const path = require('node:path')
const root = process.env.AGENTEAM_MCP_WEB_PLAYWRIGHT
if (!root || !path.isAbsolute(root) || require(path.join(root, 'package.json')).version !== '1.56.1') throw new Error('LOCKED_PLAYWRIGHT_REQUIRED')
const { defineConfig } = require(root)
const directory = process.env.AGENTEAM_MCP_WEB_PRIVATE
const executablePath = process.env.AGENTEAM_AUTH_WEB_CHROMIUM
if (![directory, executablePath].every(value => value && path.isAbsolute(value))) throw new Error('EXACT_OWNED_BROWSER_PATHS_REQUIRED')
module.exports = defineConfig({
  testDir: __dirname, testMatch: 'oauth.spec.cjs', timeout: 60000,
  expect: { timeout: 8000 }, workers: 1, retries: 0, forbidOnly: true,
  reporter: 'line', outputDir: path.join(directory, 'playwright-results'),
  use: { browserName: 'chromium', launchOptions: { executablePath, args: ['--no-sandbox'] },
    viewport: { width: 1440, height: 900 }, colorScheme: 'light',
    screenshot: 'off', video: 'off', trace: 'off' },
})
