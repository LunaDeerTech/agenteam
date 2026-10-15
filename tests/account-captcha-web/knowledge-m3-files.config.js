import { defineConfig } from '@playwright/test'
import { isAbsolute } from 'node:path'

const origin = process.env.AGENTEAM_AUTH_WEB_ORIGIN
const privateDirectory = process.env.AGENTEAM_AUTH_WEB_PRIVATE
const executablePath = process.env.AGENTEAM_AUTH_WEB_CHROMIUM
if (!origin || !privateDirectory || !executablePath) throw new Error('OWNED_KNOWLEDGE_FILES_FIXTURE_REQUIRED')
const address = new URL(origin)
if (address.protocol !== 'http:' || !['127.0.0.1', 'localhost'].includes(address.hostname) || !address.port || address.origin !== origin || ![privateDirectory, executablePath].every(isAbsolute)) throw new Error('EXACT_OWNED_KNOWLEDGE_FILES_PATHS_REQUIRED')
export default defineConfig({
  testDir: '.', testMatch: 'knowledge-m3-files.spec.js', workers: 1, retries: 0,
  timeout: 60000, expect: { timeout: 8000 }, forbidOnly: true, reporter: 'line',
  outputDir: `${privateDirectory}/knowledge-files-playwright-results`,
  use: { baseURL: origin, browserName: 'chromium', launchOptions: { executablePath, args: ['--no-sandbox'] }, screenshot: 'off', video: 'off', trace: 'off' },
})
