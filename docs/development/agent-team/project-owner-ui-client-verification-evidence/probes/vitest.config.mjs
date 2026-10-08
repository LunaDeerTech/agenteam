import { defineConfig } from 'vitest/config'
import { fileURLToPath } from 'node:url'
export default defineConfig({
  root: fileURLToPath(new URL('.', import.meta.url)),
  cacheDir: './.vite-cache',
  resolve: {
    alias: {
      '@frozen-owner/auth': fileURLToPath(new URL('./src/router/auth.ts', import.meta.url)),
    },
  },
  test: {
    environment: 'node',
    include: ['*.independent.spec.ts', 'src/tests/project-owner-client.spec.ts', 'src/tests/account-client.spec.ts'],
    pool: 'threads',
    maxWorkers: 1,
    fileParallelism: false,
    testTimeout: 3000,
    restoreMocks: true,
  },
})
