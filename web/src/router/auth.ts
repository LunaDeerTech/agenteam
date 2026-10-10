import type { Router } from 'vue-router'
import { useSession, type SessionController } from '../composables/useSession'

const returnTargets = [
  '/',
  '/projects',
  '/settings/profile',
  '/settings/appearance',
  '/settings/password',
  '/system/users',
  '/system/invitations',
  '/system/providers',
  '/system/models',
  '/system/model-selection',
  '/system/account-security',
  '/system/smtp',
  '/system/outbound-policy',
  '/system/audit',
  '/system/runtime-information',
] as const
// Inspect the raw path before Vue Router decodes params. No URL parser or decode pass.
const reservedProjectRoots = new Set([
  'api',
  'assets',
  'auth',
  'login',
  'logout',
  'invite',
  'reset',
  'settings',
  'system',
  'personal',
  'diagnostics',
  'livez',
  'readyz',
  'debug',
])
export function projectRoute(value: unknown): {
  username: string
  project_name: string
  suffix:
    | ''
    | '/settings'
    | '/settings/general'
    | '/settings/audit'
    | '/settings/secrets'
    | '/settings/model-providers'
    | '/settings/available-models'
    | '/knowledge'
    | `/knowledge/${string}`
  path: string
} | null {
  if (typeof value !== 'string' || /[%\\?#]/.test(value)) return null
  const match =
    /^\/([A-Za-z0-9][A-Za-z0-9-]{1,30}[A-Za-z0-9])\/([A-Za-z0-9._-]{1,64})(\/settings(?:\/(?:general|audit|secrets|model-providers|available-models))?|\/knowledge(?:\/[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12})?)?$/.exec(
      value,
    )
  if (!match) return null
  const username = match[1]!.toLowerCase(),
    project_name = match[2]!.toLowerCase()
  if (reservedProjectRoots.has(username) || project_name === '.' || project_name === '..')
    return null
  const suffix = (match[3] ?? '') as NonNullable<ReturnType<typeof projectRoute>>['suffix']
  return { username, project_name, suffix, path: `/${username}/${project_name}${suffix}` }
}
export function safeReturnTarget(value: unknown): string {
  if (typeof value === 'string' && (returnTargets as readonly string[]).includes(value))
    return value
  return projectRoute(value)?.path ?? '/'
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

const projectNavigation = new WeakMap<
  Router,
  {
    confirmLeave: (target?: string) => Promise<boolean>
    afterNavigation: (to: string, from: string) => void
  }
>()
export function installProjectNavigation(
  router: Router,
  owner: {
    confirmLeave: (target?: string) => Promise<boolean>
    afterNavigation: (to: string, from: string) => void
  },
) {
  projectNavigation.set(router, owner)
  return () => {
    if (projectNavigation.get(router) === owner) projectNavigation.delete(router)
  }
}

const projectModelNavigation = new WeakMap<
  Router,
  {
    confirmLeave: (target?: string) => Promise<boolean>
    afterNavigation: (to: string, from: string) => void
  }
>()
export function installProjectModelSettingsNavigation(
  router: Router,
  owner: {
    confirmLeave: (target?: string) => Promise<boolean>
    afterNavigation: (to: string, from: string) => void
  },
) {
  projectModelNavigation.set(router, owner)
  return () => {
    if (projectModelNavigation.get(router) === owner) projectModelNavigation.delete(router)
  }
}

const projectSecretsNavigation = new WeakMap<Router, { confirmLeave: () => Promise<boolean> }>()
export function installProjectSecretsNavigation(
  router: Router,
  owner: { confirmLeave: () => Promise<boolean> },
) {
  projectSecretsNavigation.set(router, owner)
  return () => {
    if (projectSecretsNavigation.get(router) === owner) projectSecretsNavigation.delete(router)
  }
}

export function installAuthentication(router: Router, auth: SessionController = useSession()) {
  router.beforeEach(async (to, from) => {
    if (to.meta.projectWorkspace && to.fullPath !== '/projects' && !projectRoute(to.fullPath))
      return {
        name: 'not-found',
        params: { pathMatch: to.fullPath.slice(1).split('/') },
        replace: true,
      }
    if (
      from.meta.projectWorkspace &&
      to.fullPath !== from.fullPath &&
      !((await projectNavigation.get(router)?.confirmLeave(to.fullPath)) ?? true)
    )
      return false
    if (
      to.fullPath !== from.fullPath &&
      !((await projectModelNavigation.get(router)?.confirmLeave(to.fullPath)) ?? true)
    )
      return false
    const secretNavigation = projectSecretsNavigation.get(router)
    if (
      secretNavigation &&
      to.fullPath !== from.fullPath &&
      !(await secretNavigation.confirmLeave())
    )
      return false
    // Ask before Session revalidation can temporarily unmount the dirty page.
    if (
      from.path === '/system/outbound-policy' &&
      to.fullPath !== from.fullPath &&
      !((await outboundPolicyNavigation.get(router)?.confirmLeave()) ?? true)
    )
      return false
    if (
      from.path === '/system/smtp' &&
      to.fullPath !== from.fullPath &&
      !((await smtpNavigation.get(router)?.confirmLeave()) ?? true)
    )
      return false
    if (
      from.path === '/system/account-security' &&
      to.fullPath !== from.fullPath &&
      !((await accountSecurityNavigation.get(router)?.confirmLeave()) ?? true)
    )
      return false
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
    if (!failure) projectNavigation.get(router)?.afterNavigation(to.fullPath, from.fullPath)
    if (!failure) projectModelNavigation.get(router)?.afterNavigation(to.fullPath, from.fullPath)
    if (!failure) outboundPolicyNavigation.get(router)?.afterNavigation(to.fullPath, from.fullPath)
    if (!failure) smtpNavigation.get(router)?.afterNavigation(to.fullPath, from.fullPath)
    if (!failure) accountSecurityNavigation.get(router)?.afterNavigation(to.fullPath, from.fullPath)
    if (!failure) modelSelectionNavigation.get(router)?.afterNavigation(to.fullPath, from.fullPath)
    if (!failure) modelNavigation.get(router)?.afterNavigation(to.fullPath, from.fullPath)
    if (!failure) providerNavigation.get(router)?.afterNavigation(to.fullPath, from.fullPath)
    if (!failure) invitationNavigation.get(router)?.afterNavigation(to.fullPath, from.fullPath)
    if (!failure) personalNavigation.get(router)?.afterNavigation(to.fullPath, from.fullPath)
    if (!failure && from.meta.authentication && !to.meta.authentication) auth.leave()
    if (!failure) accountEntryNavigation.get(router)?.afterNavigation(to.fullPath, from.fullPath)
  })
}

const accountSecurityNavigation = new WeakMap<
  Router,
  {
    confirmLeave: () => Promise<boolean>
    afterNavigation: (to: string, from: string) => void
  }
>()
export function installAccountSecurityNavigation(
  router: Router,
  owner: {
    confirmLeave: () => Promise<boolean>
    afterNavigation: (to: string, from: string) => void
  },
) {
  accountSecurityNavigation.set(router, owner)
  return () => {
    if (accountSecurityNavigation.get(router) === owner) accountSecurityNavigation.delete(router)
  }
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

const smtpNavigation = new WeakMap<
  Router,
  { confirmLeave: () => Promise<boolean>; afterNavigation: (to: string, from: string) => void }
>()
export function installSMTPSettingsNavigation(
  router: Router,
  owner: {
    confirmLeave: () => Promise<boolean>
    afterNavigation: (to: string, from: string) => void
  },
) {
  smtpNavigation.set(router, owner)
  return () => {
    if (smtpNavigation.get(router) === owner) smtpNavigation.delete(router)
  }
}

const outboundPolicyNavigation = new WeakMap<
  Router,
  { confirmLeave: () => Promise<boolean>; afterNavigation: (to: string, from: string) => void }
>()
export function installOutboundPolicyNavigation(
  router: Router,
  owner: {
    confirmLeave: () => Promise<boolean>
    afterNavigation: (to: string, from: string) => void
  },
) {
  outboundPolicyNavigation.set(router, owner)
  return () => {
    if (outboundPolicyNavigation.get(router) === owner) outboundPolicyNavigation.delete(router)
  }
}
