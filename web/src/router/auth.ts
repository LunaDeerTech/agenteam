import type { Router } from 'vue-router'
import { useSession, type SessionController } from '../composables/useSession'

// Only the currently implemented, protected homepage is a return destination.
export function safeReturnTarget(value: unknown): '/' {
  return value === '/' ? value : '/'
}

export function installAuthentication(router: Router, auth: SessionController = useSession()) {
  router.beforeEach(async (to) => {
    if (!to.meta.authentication) return true
    await auth.restore()
    if (to.meta.protected && auth.state.phase !== 'authenticated') {
      return { name: 'login', query: { return: '/' }, replace: true }
    }
    if (to.name === 'login' && auth.state.phase === 'authenticated')
      return safeReturnTarget(to.query.return)
    return true
  })
  router.afterEach((to, from, failure) => {
    if (!failure && from.meta.authentication && !to.meta.authentication) auth.leave()
  })
}
