import type { Router } from 'vue-router'
import { useSession, type SessionController } from '../composables/useSession'

const returnTargets = [
  '/',
  '/settings/profile',
  '/settings/appearance',
  '/settings/password',
] as const
type ReturnTarget = (typeof returnTargets)[number]
export function safeReturnTarget(value: unknown): ReturnTarget {
  return typeof value === 'string' && returnTargets.includes(value as ReturnTarget)
    ? (value as ReturnTarget)
    : '/'
}

const personalNavigation = new WeakMap<
  Router,
  { confirmLeave: () => Promise<boolean>; afterNavigation: (to: string, from: string) => void }
>()
export function installPersonalNavigation(
  router: Router,
  owner: {
    confirmLeave: () => Promise<boolean>
    afterNavigation: (to: string, from: string) => void
  },
) {
  personalNavigation.set(router, owner)
  return () => {
    if (personalNavigation.get(router) === owner) personalNavigation.delete(router)
  }
}

export function installAuthentication(router: Router, auth: SessionController = useSession()) {
  router.beforeEach(async (to, from) => {
    // Ask before Session revalidation can temporarily unmount the dirty page.
    if (
      from.path.startsWith('/settings') &&
      to.fullPath !== from.fullPath &&
      !((await personalNavigation.get(router)?.confirmLeave()) ?? true)
    )
      return false
    if (!to.meta.authentication) return true
    await auth.restore()
    if (to.meta.protected && auth.state.phase !== 'authenticated') {
      return { name: 'login', query: { return: safeReturnTarget(to.fullPath) }, replace: true }
    }
    if (to.name === 'login' && auth.state.phase === 'authenticated')
      return safeReturnTarget(to.query.return)
    return true
  })
  router.afterEach((to, from, failure) => {
    if (!failure) personalNavigation.get(router)?.afterNavigation(to.fullPath, from.fullPath)
    if (!failure && from.meta.authentication && !to.meta.authentication) auth.leave()
  })
}
