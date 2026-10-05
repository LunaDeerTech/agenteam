import { defineConfig } from 'vitest/config'

export default defineConfig({
  cacheDir: '/workspace/agenteam-d26-pure-v-1z5m6v_m/cache/vite',
  test: {
    environment: 'jsdom',
    include: ['src/tests/d26-independent.spec.ts'],
    restoreMocks: true,
    watch: false,
    pool: 'forks',
    maxWorkers: 1,
  },
})
