import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'
export default defineConfig(({ command }) => {
  const value = command === 'serve' ? process.env.AGENTEAM_DEV_API_TARGET : undefined
  let target: string | undefined
  if (value) {
    const url = new URL(value)
    if (
      url.protocol !== 'http:' ||
      !['127.0.0.1', '[::1]', 'localhost'].includes(url.hostname) ||
      !url.port ||
      url.username ||
      url.password ||
      url.search ||
      url.hash ||
      url.pathname !== '/'
    ) {
      throw new Error('AGENTEAM_DEV_API_TARGET must be an owned loopback HTTP origin')
    }
    target = url.origin
  }
  return {
    plugins: [vue()],
    server: {
      host: '127.0.0.1',
      port: 5173,
      strictPort: true,
      ...(target ? { proxy: { '/api/v1': { target, changeOrigin: false, ws: false } } } : {}),
    },
    test: { environment: 'jsdom', include: ['src/tests/**/*.spec.ts'], restoreMocks: true },
  }
})
