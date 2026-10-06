import type { Router } from 'vue-router'
import { useSession, type SessionController } from '../composables/useSession'

const returnTargets = [
  '/',
  '/settings/profile',
  '/settings/appearance',
  '/settings/password',
  '/system/users',
  '/system/invitations',
  '/system/providers',
  '/system/models',
  '/system/model-selection',
] as const
type ReturnTarget = (typeof returnTargets)[number]
export function safeReturnTarget(value: unknown): ReturnTarget {
  return typeof value === 'string' && returnTargets.includes(value as ReturnTarget)
    ? (value as ReturnTarget)
    : '/'
}
export function isAccountSwitch(value: unknown): boolean {
  return value === '1'
}
const accountEntryNavigation = new WeakMap<
  Router,
  {
    confirmLeave: () => Promise<boolean>
    afterNavigation: (to: string, from: string) => void
  }
>()
export function installAccountEntryNavigation(
  router: Router,
  owner: {
    confirmLeave: () => Promise<boolean>
    afterNavigation: (to: string, from: string) => void
  },
) {
  accountEntryNavigation.set(router, owner)
  return () => {
    if (accountEntryNavigation.get(router) === owner) accountEntryNavigation.delete(router)
  }
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
      from.path === '/system/model-selection' &&
      to.fullPath !== from.fullPath &&
      !((await modelSelectionNavigation.get(router)?.confirmLeave()) ?? true)
    )
      return false
    if (
      from.path === '/system/models' &&
      to.fullPath !== from.fullPath &&
      !((await modelNavigation.get(router)?.confirmLeave()) ?? true)
    )
      return false
    if (
      from.path === '/system/providers' &&
      to.fullPath !== from.fullPath &&
      !((await providerNavigation.get(router)?.confirmLeave()) ?? true)
    )
      return false
    if (
      from.path === '/system/invitations' &&
      to.fullPath !== from.fullPath &&
      !((await invitationNavigation.get(router)?.confirmLeave()) ?? true)
    )
      return false
    if (
      from.path.startsWith('/settings') &&
      to.fullPath !== from.fullPath &&
      !((await personalNavigation.get(router)?.confirmLeave()) ?? true)
    )
      return false
    if (from.meta.accountEntry && to.fullPath !== from.fullPath) {
      if (!((await accountEntryNavigation.get(router)?.confirmLeave()) ?? true) || auth.state.busy)
        return false
      auth.entry.abandon()
    }
    if (to.meta.accountEntry) {
      if (auth.state.busy) return false
      if (from.name === 'login') auth.leave()
      return true
    }
    if (!to.meta.authentication) return true
    await auth.restore()
    if (to.meta.protected && auth.state.phase !== 'authenticated') {
      return { name: 'login', query: { return: safeReturnTarget(to.fullPath) }, replace: true }
    }
    if (
      to.name === 'login' &&
      auth.state.phase === 'authenticated' &&
      !isAccountSwitch(to.query.switch)
    )
      return safeReturnTarget(to.query.return)
    return true
  })
  router.afterEach((to, from, failure) => {
    if (!failure) modelSelectionNavigation.get(router)?.afterNavigation(to.fullPath, from.fullPath)
    if (!failure) modelNavigation.get(router)?.afterNavigation(to.fullPath, from.fullPath)
    if (!failure) providerNavigation.get(router)?.afterNavigation(to.fullPath, from.fullPath)
    if (!failure) invitationNavigation.get(router)?.afterNavigation(to.fullPath, from.fullPath)
    if (!failure) personalNavigation.get(router)?.afterNavigation(to.fullPath, from.fullPath)
    if (!failure && from.meta.authentication && !to.meta.authentication) auth.leave()
    if (!failure) accountEntryNavigation.get(router)?.afterNavigation(to.fullPath, from.fullPath)
  })
}

const invitationNavigation = new WeakMap<
  Router,
  {
    confirmLeave: () => Promise<boolean>
    afterNavigation: (to: string, from: string) => void
  }
>()
export function installInvitationNavigation(
  router: Router,
  owner: {
    confirmLeave: () => Promise<boolean>
    afterNavigation: (to: string, from: string) => void
  },
) {
  invitationNavigation.set(router, owner)
  return () => {
    if (invitationNavigation.get(router) === owner) invitationNavigation.delete(router)
  }
}

const providerNavigation = new WeakMap<
  Router,
  {
    confirmLeave: () => Promise<boolean>
    afterNavigation: (to: string, from: string) => void
  }
>()
export function installProviderNavigation(
  router: Router,
  owner: {
    confirmLeave: () => Promise<boolean>
    afterNavigation: (to: string, from: string) => void
  },
) {
  providerNavigation.set(router, owner)
  return () => {
    if (providerNavigation.get(router) === owner) providerNavigation.delete(router)
  }
}

const modelNavigation = new WeakMap<
  Router,
  {
    confirmLeave: () => Promise<boolean>
    afterNavigation: (to: string, from: string) => void
  }
>()
export function installModelNavigation(
  router: Router,
  owner: {
    confirmLeave: () => Promise<boolean>
    afterNavigation: (to: string, from: string) => void
  },
) {
  modelNavigation.set(router, owner)
  return () => {
    if (modelNavigation.get(router) === owner) modelNavigation.delete(router)
  }
}

const modelSelectionNavigation = new WeakMap<
  Router,
  { confirmLeave: () => Promise<boolean>; afterNavigation: (to: string, from: string) => void }
>()
export function installModelSelectionNavigation(
  router: Router,
  owner: {
    confirmLeave: () => Promise<boolean>
    afterNavigation: (to: string, from: string) => void
  },
) {
  modelSelectionNavigation.set(router, owner)
  return () => {
    if (modelSelectionNavigation.get(router) === owner) modelSelectionNavigation.delete(router)
  }
}
