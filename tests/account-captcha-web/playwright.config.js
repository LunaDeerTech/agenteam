import { defineConfig } from '@playwright/test'
if (!process.env.ACCOUNT_CAPTCHA_BASE_URL) throw new Error('OWNED_ACCOUNT_FIXTURE_REQUIRED')
const fixtureCase = process.env.ACCOUNT_CAPTCHA_CASE
if (fixtureCase !== 'desktop' && fixtureCase !== 'keyboard') throw new Error('EXACT_ACCOUNT_CASE_REQUIRED')
export default defineConfig({
 testDir: './e2e', timeout: 45_000, workers: 1, retries: 0, reporter: 'line',
 grep: new RegExp(`\\[${fixtureCase}\\]`),
 use: { baseURL: process.env.ACCOUNT_CAPTCHA_BASE_URL, browserName: 'chromium', launchOptions: { executablePath: '/usr/bin/chromium', args: ['--no-sandbox'] }, screenshot: 'off', video: 'off', trace: 'off' },
})
