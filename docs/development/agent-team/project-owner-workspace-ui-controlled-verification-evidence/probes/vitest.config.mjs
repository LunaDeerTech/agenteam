import { defineConfig } from 'vitest/config'

export default defineConfig({
  root: '/workspace/scratch/owner-ui-verification/ui-v1',
  cacheDir: '/workspace/scratch/owner-ui-verification/ui-v1/.vite',
  test: {
    environment: 'jsdom',
    include: ['*.independent.spec.ts'],
    pool: 'threads',
    maxWorkers: 1,
    fileParallelism: false,
    restoreMocks: true,
  },
})
