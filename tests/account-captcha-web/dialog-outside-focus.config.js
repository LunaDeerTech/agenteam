import { defineConfig } from '@playwright/test'
import { isAbsolute, join } from 'node:path'

for (const name of [
  'AGENTEAM_DIALOG_WEB_ROOT',
  'AGENTEAM_DIALOG_RUN_DIR',
  'AGENTEAM_DIALOG_CHROMIUM',
]) {
  if (!process.env[name] || !isAbsolute(process.env[name])) throw new Error(`${name}_REQUIRED`)
}

export default defineConfig({
  testDir: './e2e',
  testMatch: 'dialog-outside-focus.spec.ts',
  timeout: 45_000,
  expect: { timeout: 3_000 },
  workers: 1,
  retries: 0,
  forbidOnly: true,
  outputDir: join(process.env.AGENTEAM_DIALOG_RUN_DIR, 'results'),
  reporter: [
    ['line'],
    ['json', { outputFile: join(process.env.AGENTEAM_DIALOG_RUN_DIR, 'results.json') }],
  ],
  use: {
    browserName: 'chromium',
    serviceWorkers: 'block',
    launchOptions: {
      executablePath: process.env.AGENTEAM_DIALOG_CHROMIUM,
      args: ['--no-sandbox', '--disable-dev-shm-usage'],
    },
    screenshot: 'only-on-failure',
    trace: 'retain-on-failure',
    video: 'off',
  },
})
