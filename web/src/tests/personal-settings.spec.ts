import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import type { AccountAPI, AvatarMetadata, SessionView } from '../api/account'
import { AccountFailure, type Problem } from '../api/client'
import type { SessionController } from '../composables/useSession'
const selected = vi.hoisted(() => ({ auth: null as SessionController | null }))
vi.mock('../composables/useSession', async (original) => ({
  ...(await original<typeof import('../composables/useSession')>()),
  useSession: () => selected.auth!,
}))
import { createSessionController } from '../composables/useSession'
import { useTheme } from '../composables/useTheme'
import { installAuthentication, safeReturnTarget } from '../router/auth'
import App from '../App.vue'
import HomeView from '../views/HomeView.vue'
import LoginView from '../views/auth/LoginView.vue'
import PersonalSettingsView from '../views/settings/PersonalSettingsView.vue'
import ProfileSettings from '../views/settings/ProfileSettings.vue'
import AppearanceSettings from '../views/settings/AppearanceSettings.vue'
import PasswordSettings from '../views/settings/PasswordSettings.vue'

const id = '01900000-0000-7000-8000-000000000001',
  session = '01900000-0000-7000-8000-000000000002',
  rotated = '01900000-0000-7000-8000-000000000003'
const time = '2026-10-05T12:34:56.123456Z'
function problem(
  code: string,
  status = 409,
  state: Problem['commit_state'] = 'not_started',
  path?: string,
) {
  return new AccountFailure('problem', {
    type: 'urn:agenteam:problem:test',
    title: 'Test',
    status,
    detail: 'Safe message',
    instance: '/api/v1/me',
    code,
    request_id: id,
    commit_state: state,
    ...(path ? { field_errors: [{ path, code: 'INVALID' }] } : {}),
  })
}
function deferred<T>() {
  let resolve!: (v: T) => void
  const promise = new Promise<T>((r) => {
    resolve = r
  })
  return { promise, resolve }
}
let wrapper: VueWrapper | undefined
let urls = 0
const created = vi.fn(),
  revoked = vi.fn()
beforeEach(() => {
  vi.stubGlobal(
    'matchMedia',
    vi.fn((query: string) => ({
      matches: false,
      media: query,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  )
  class TestURL extends URL {
    static createObjectURL(blob: Blob) {
      const value = `blob:owned-${++urls}`
      created(value, blob)
      return value
    }
    static revokeObjectURL(value: string) {
      revoked(value)
    }
  }
  vi.stubGlobal('URL', TestURL)
  created.mockClear()
  revoked.mockClear()
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  document.body.innerHTML = ''
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  useTheme().setTheme('system')
})
async function page(path = '/settings/profile', anonymous = false) {
  let signedIn = !anonymous
  let current: SessionView = {
    user: {
      id,
      email: 'person@example.com',
      username: 'admin',
      display_name: 'Person',
      role: 'user',
      theme: 'dark',
      version: '1',
      initial_password_suggestion: true,
    },
    session: { id: session, issued_at: time, idle_expires_at: time, absolute_expires_at: time },
    csrf_token: 'S'.repeat(43),
  }
  let avatar: AvatarMetadata | null = null
  const copy = <T>(value: T): T => JSON.parse(JSON.stringify(value))
  const userChanged = (value: Partial<SessionView['user']>) => {
    current = {
      ...current,
      user: { ...current.user, ...value, version: String(BigInt(current.user.version) + 1n) },
    }
  }
  const unexpected = async (): Promise<never> => {
    throw new Error('unexpected Account request')
  }
  const api = {
    getSession: vi.fn<AccountAPI['getSession']>(async () => {
      if (!signedIn) throw problem('UNAUTHENTICATED', 401)
      return copy(current)
    }),
    inspectInvitation: vi.fn<AccountAPI['inspectInvitation']>(async () => {
      throw new Error('unexpected public entry request')
    }),
    redeemInvitation: vi.fn<AccountAPI['redeemInvitation']>(async () => {
      throw new Error('unexpected public entry request')
    }),
    requestPasswordReset: vi.fn<AccountAPI['requestPasswordReset']>(async () => {
      throw new Error('unexpected public entry request')
    }),
    inspectPasswordReset: vi.fn<AccountAPI['inspectPasswordReset']>(async () => {
      throw new Error('unexpected public entry request')
    }),
    completePasswordReset: vi.fn<AccountAPI['completePasswordReset']>(async () => {
      throw new Error('unexpected public entry request')
    }),
    bootstrap: vi.fn<AccountAPI['bootstrap']>(async () => ({
      csrf_token: 'A'.repeat(43),
      challenge_modes: ['rotate'],
      delivery_channel: 'backend_log',
    })),
    login: vi.fn<AccountAPI['login']>(async () => {
      signedIn = true
      return { user: copy(current.user), session: copy(current.session), next_path: '/' }
    }),
    logout: vi.fn<AccountAPI['logout']>(async () => {
      signedIn = false
    }),
    createChallenge: vi.fn<AccountAPI['createChallenge']>(unexpected),
    verifyChallenge: vi.fn<AccountAPI['verifyChallenge']>(unexpected),
    getProfile: vi.fn<AccountAPI['getProfile']>(async () => ({
      user: copy(current.user),
      avatar: copy(avatar),
    })),
    updateProfile: vi.fn<AccountAPI['updateProfile']>(async (input) => {
      userChanged({
        ...(input.username === undefined ? {} : { username: input.username.toLowerCase() }),
        ...(input.display_name === undefined ? {} : { display_name: input.display_name }),
      })
      return { user: copy(current.user), avatar: copy(avatar) }
    }),
    getPreferences: vi.fn<AccountAPI['getPreferences']>(async () => ({
      version: current.user.version,
      theme: current.user.theme,
    })),
    setPreferences: vi.fn<AccountAPI['setPreferences']>(async (input) => {
      userChanged({ theme: input.theme })
      return { version: current.user.version, theme: current.user.theme }
    }),
    putAvatar: vi.fn<AccountAPI['putAvatar']>(async () => {
      avatar = { media_type: 'image/png', byte_size: '4', sha256: 'sha256:' + 'a'.repeat(64) }
      userChanged({})
      return { user: copy(current.user), avatar: copy(avatar) }
    }),
    deleteAvatar: vi.fn<AccountAPI['deleteAvatar']>(async () => {
      avatar = null
      userChanged({})
    }),
    readAvatar: vi.fn<AccountAPI['readAvatar']>(async () => {
      if (!avatar) throw problem('NOT_FOUND', 404)
      return {
        metadata: copy(avatar),
        blob: new Blob([new Uint8Array(4)], { type: avatar.media_type }),
      }
    }),
    changePassword: vi.fn<AccountAPI['changePassword']>(async () => {
      userChanged({ initial_password_suggestion: false })
      current = {
        ...current,
        session: { ...current.session, id: rotated },
        csrf_token: 'N'.repeat(43),
      }
      return { completed: true, next_path: '/' }
    }),
  }
  selected.auth = createSessionController(api)
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: HomeView, meta: { protected: true, authentication: true } },
      { path: '/login', name: 'login', component: LoginView, meta: { authentication: true } },
      {
        path: '/settings',
        component: PersonalSettingsView,
        redirect: '/settings/profile',
        meta: { protected: true, authentication: true },
        children: [
          { path: 'profile', component: ProfileSettings },
          { path: 'appearance', component: AppearanceSettings },
          { path: 'password', component: PasswordSettings },
        ],
      },
      { path: '/elsewhere', component: { template: '<p>Elsewhere</p>' } },
    ],
  })
  installAuthentication(router, selected.auth)
  await router.push(path)
  await router.isReady()
  wrapper = mount(App, { attachTo: document.body, global: { plugins: [router] } })
  await flushPromises()
  return {
    api,
    auth: selected.auth,
    router,
    wrapper,
    userChanged,
    current: () => copy(current),
    invalidate: () => {
      signedIn = false
    },
  }
}
const button = (text: string) => {
  const element = [...document.querySelectorAll('button')].find(
    (b) =>
      (
        b.querySelector('.button-label[aria-hidden="false"]')?.textContent ?? b.textContent
      )?.trim() === text,
  )
  if (!element) throw new Error(`button missing: ${text}`)
  return element
}
async function click(text: string) {
  button(text).click()
  await flushPromises()
}

describe('personal settings pages', () => {
  it('submits the last native/autofill value without changing readonly fields or trimming display names', async () => {
    const p = await page('/settings')
    expect(p.router.currentRoute.value.path).toBe('/settings/profile')
    expect(p.wrapper.get('#profile-email').attributes('readonly')).toBeDefined()
    const input = p.wrapper.get('#profile-display-name').element as HTMLInputElement
    input.value = ' 最后字符🙂 '
    await p.wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(p.api.updateProfile.mock.calls.length).toBe(1)
    expect(p.api.updateProfile.mock.calls[0]![0]).toEqual({
      version: '1',
      display_name: ' 最后字符🙂 ',
    })
    expect(p.wrapper.get('.account-name').text()).toBe('最后字符🙂')
    await p.wrapper.get('#profile-display-name').setValue('')
    await p.wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(p.api.updateProfile.mock.calls[1]![0]).toEqual({ version: '2', display_name: '' })
    expect(p.wrapper.get('.account-name').text()).toBe('person@example.com')
  })
  it('asks before route revalidation or logout, preserves edit/focus on continue and discards only on consent', async () => {
    const p = await page()
    await p.wrapper.get('#profile-display-name').setValue('draft')
    const focus = p.wrapper.get('#profile-display-name').element as HTMLElement
    focus.focus()
    const before = p.api.getSession.mock.calls.length
    const navigation = p.router.push('/settings/appearance')
    await flushPromises()
    expect(document.querySelector('[role="dialog"]')).not.toBeNull()
    expect(p.api.getSession.mock.calls.length).toBe(before)
    await click('继续编辑')
    await navigation
    expect(p.router.currentRoute.value.path).toBe('/settings/profile')
    expect(p.wrapper.get('#profile-display-name').element).toHaveProperty('value', 'draft')
    expect(document.activeElement).toBe(focus)
    p.wrapper
      .get('.account-actions button')
      .element.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await flushPromises()
    expect(p.api.logout.mock.calls.length).toBe(0)
    await click('放弃修改')
    expect(p.api.logout.mock.calls.length).toBe(1)
    expect(p.router.currentRoute.value.path).toBe('/login')
  })
  it('keeps the same draft and theme preview through hidden Session revalidation and cancels to the latest saved value', async () => {
    const p = await page('/settings/appearance')
    await p.wrapper.get('input[value="light"]').setValue(true)
    expect(useTheme().mode.value).toBe('light')
    expect(p.api.setPreferences.mock.calls.length).toBe(0)
    p.userChanged({ theme: 'system' })
    const hold = deferred<SessionView>()
    p.api.getSession.mockReturnValueOnce(hold.promise)
    const check = p.auth.restore()
    await flushPromises()
    expect(p.wrapper.find('input[value="light"]').exists()).toBe(false)
    expect(useTheme().mode.value).toBe('light')
    hold.resolve(p.current())
    await check
    await flushPromises()
    expect(p.wrapper.get('input[value="light"]').element).toHaveProperty('checked', true)
    expect(p.wrapper.text()).toContain('原版本仍保留')
    expect(useTheme().mode.value).toBe('light')
    await click('取消预览')
    expect(useTheme().mode.value).toBe('system')
    expect(p.api.setPreferences.mock.calls.length).toBe(0)
  })
  it('preserves the original dirty version after conflict and requires explicit reload consent', async () => {
    const p = await page()
    await p.wrapper.get('#profile-display-name').setValue('draft')
    p.api.updateProfile.mockRejectedValueOnce(problem('VERSION_CONFLICT'))
    await p.wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(p.wrapper.get('#profile-display-name').element).toHaveProperty('value', 'draft')
    expect(p.wrapper.text()).toContain('版本已变化')
    p.userChanged({ display_name: 'new current' })
    await click('重新加载最新资料')
    expect(document.querySelector('[role="dialog"]')).not.toBeNull()
    await click('继续编辑')
    expect(p.wrapper.get('#profile-display-name').element).toHaveProperty('value', 'draft')
    await click('重新加载最新资料')
    await click('放弃修改')
    expect(p.wrapper.get('#profile-display-name').element).toHaveProperty('value', 'new current')
  })
  it('keeps selected avatar independent of dirty profile, reads confirmed bytes and revokes every owned URL on leave', async () => {
    const p = await page()
    await p.wrapper.get('#profile-display-name').setValue('unsaved profile')
    const file = new File(['selected bytes'], 'portrait.png', { type: 'image/png' })
    const input = p.wrapper.get('input[type="file"]').element as HTMLInputElement
    Object.defineProperty(input, 'files', { configurable: true, value: [file] })
    await p.wrapper.get('input[type="file"]').trigger('change')
    expect(p.wrapper.find('img[alt="尚未上传的头像预览"]').exists()).toBe(true)
    expect(p.api.putAvatar.mock.calls.length).toBe(0)
    await click('上传 / 替换头像')
    expect(p.api.putAvatar.mock.calls.length).toBe(1)
    expect(p.api.putAvatar.mock.calls[0]![0].file === file).toBe(true)
    expect(p.wrapper.find('img[alt="当前已保存头像"]').exists()).toBe(true)
    expect(p.wrapper.find('img[alt="尚未上传的头像预览"]').exists()).toBe(false)
    expect(p.wrapper.get('#profile-display-name').element).toHaveProperty(
      'value',
      'unsaved profile',
    )
    expect(p.api.updateProfile.mock.calls.length).toBe(0)
    expect(created.mock.calls.length).toBe(2)
    const leave = p.router.push('/')
    await flushPromises()
    await click('放弃修改')
    await leave
    await flushPromises()
    // Session revalidation can briefly remount the current leaf before navigation
    // commits. Its readback URL has the same exact cleanup obligation.
    expect(revoked.mock.calls.map(([url]) => url).sort()).toEqual(
      created.mock.calls.map(([url]) => url).sort(),
    )
  })
  it('clears all password fields at the exact 200 while the same-owner Session confirmation remains held', async () => {
    const p = await page('/settings/password')
    const check = deferred<SessionView>()
    p.api.getSession.mockReturnValueOnce(check.promise)
    await p.wrapper.get('#settings-current_password').setValue('Original password 123')
    await p.wrapper.get('#settings-new_password').setValue(' New password 字符 123 ')
    await p.wrapper.get('#settings-confirmation').setValue(' New password 字符 123 ')
    await p.wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(p.auth.personal.passwordProgress?.phase).toBe('confirming-session')
    for (const field of ['current_password', 'new_password', 'confirmation']) {
      expect(p.wrapper.get(`#settings-${field}`).element).toHaveProperty('value', '')
      expect(p.wrapper.get(`#settings-${field}`).attributes('maxlength')).toBeUndefined()
    }
    expect(p.auth.state.busy).toBe(true)
    check.resolve(p.current())
    await flushPromises()
    expect(p.wrapper.text()).toContain('其他旧登录已失效')
    expect(p.router.currentRoute.value.path).toBe('/settings/password')
    expect(p.auth.state.session?.id).toBe(rotated)
    expect(p.api.changePassword.mock.calls.length).toBe(1)
  })
  it('retains confirmed password feedback after a failed Session confirmation and a successful explicit check', async () => {
    const p = await page('/settings/password')
    p.api.getSession.mockRejectedValueOnce(problem('DEPENDENCY_UNAVAILABLE', 503, 'not_started'))
    await p.wrapper.get('#settings-current_password').setValue('Original password 123')
    await p.wrapper.get('#settings-new_password').setValue(' New password 字符 123 ')
    await p.wrapper.get('#settings-confirmation').setValue(' New password 字符 123 ')
    await p.wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(p.api.changePassword.mock.calls.length).toBe(1)
    expect(p.auth.personal.passwordProgress?.phase).toBe('session-unconfirmed')
    expect(p.auth.state.phase).toBe('unavailable')
    expect(p.wrapper.text()).toContain('密码修改已确认')
    expect(p.wrapper.find('#settings-current_password').exists()).toBe(false)
    await click('检查当前会话')
    expect(p.auth.state.phase).toBe('authenticated')
    expect(p.auth.state.session?.id).toBe(rotated)
    expect(p.auth.personal.passwordProgress?.phase).toBe('confirmed')
    expect(p.api.changePassword.mock.calls.length).toBe(1)
    expect(p.router.currentRoute.value.path).toBe('/settings/password')
    for (const name of ['current_password', 'new_password', 'confirmation'])
      expect(p.wrapper.get(`#settings-${name}`).element).toHaveProperty('value', '')
    expect(p.wrapper.text()).toContain('密码已修改，当前登录已更新，其他旧登录已失效。')
    await p.router.push('/settings/appearance')
    await flushPromises()
    p.api.getProfile.mockRejectedValueOnce(problem('DEPENDENCY_UNAVAILABLE', 503, 'not_started'))
    await p.router.push('/settings/password')
    await flushPromises()
    expect(p.wrapper.text()).toContain('密码已修改，当前登录已更新，其他旧登录已失效。')
    expect(p.wrapper.text()).toContain('操作未完成，请检查当前登录状态或重试。')
    expect(p.api.changePassword.mock.calls.length).toBe(1)
  })

  it('retires confirmed password feedback on a later same-user Session epoch', async () => {
    const p = await page('/settings/password')
    await p.wrapper.get('#settings-current_password').setValue('Original password 123')
    await p.wrapper.get('#settings-new_password').setValue(' New password 字符 123 ')
    await p.wrapper.get('#settings-confirmation').setValue(' New password 字符 123 ')
    await p.wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(p.auth.state.session?.id).toBe(rotated)
    expect(p.wrapper.text()).toContain('密码已修改，当前登录已更新，其他旧登录已失效。')
    await p.auth.restore()
    await flushPromises()
    expect(p.wrapper.text()).toContain('密码已修改，当前登录已更新，其他旧登录已失效。')
    const priorEpoch = p.auth.personalContext.identity?.epoch
    const later = {
      ...p.current(),
      session: { ...p.current().session, id: '01900000-0000-7000-8000-000000000004' },
    }
    p.api.getSession.mockResolvedValueOnce(later)
    await p.auth.restore()
    await flushPromises()
    expect(p.auth.state.session?.id).toBe(later.session.id)
    expect(p.auth.personalContext.identity?.epoch).not.toBe(priorEpoch)
    expect(p.api.changePassword.mock.calls.length).toBe(1)
    for (const name of ['current_password', 'new_password', 'confirmation'])
      expect(p.wrapper.get(`#settings-${name}`).element).toHaveProperty('value', '')
    expect(p.wrapper.text()).not.toContain('密码已修改，当前登录已更新，其他旧登录已失效。')
  })

  it('uses section errors for ambiguous password paths and preserves ordinary failure inputs', async () => {
    const p = await page('/settings/password')
    p.api.changePassword.mockRejectedValueOnce(
      problem('VALIDATION_FAILED', 422, 'not_started', '/password'),
    )
    await p.wrapper.get('#settings-current_password').setValue('Original password 123')
    await p.wrapper.get('#settings-new_password').setValue(' New password 字符 123 ')
    await p.wrapper.get('#settings-confirmation').setValue(' New password 字符 123 ')
    await p.wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(p.wrapper.text()).toContain('密码校验失败')
    expect(p.wrapper.get('#settings-current_password').attributes('aria-invalid')).not.toBe('true')
    expect(
      (p.wrapper.get('#settings-new_password').element as HTMLInputElement).value ===
        ' New password 字符 123 ',
    ).toBe(true)
  })
  it.each(['/settings/profile', '/settings/appearance', '/settings/password'])(
    'returns through real controller login to the approved leaf %s',
    async (target) => {
      const p = await page(target, true)
      expect(p.router.currentRoute.value.path).toBe('/login')
      expect(p.router.currentRoute.value.query.return).toBe(target)
      await p.wrapper.get('#login-email').setValue('person@example.com')
      await p.wrapper.get('#login-password').setValue('Original password 123')
      await p.wrapper.get('form').trigger('submit')
      await flushPromises()
      expect(p.router.currentRoute.value.path).toBe(target)
      expect(p.api.login.mock.calls.length).toBe(1)
    },
  )
  it('only permits the five exact return targets and clears dirty previews on real context invalidation', async () => {
    expect(safeReturnTarget('/system/users')).toBe('/system/users')
    for (const target of [
      '/settings',
      '/settings/profile?x=1',
      '/settings/password#x',
      '//example.com',
      'https://example.com',
      ['/settings/profile'],
    ])
      expect(safeReturnTarget(target)).toBe('/')
    const p = await page('/settings/appearance')
    await p.wrapper.get('input[value="light"]').setValue(true)
    const event = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(event)
    expect(event.defaultPrevented).toBe(true)
    p.invalidate()
    await p.auth.restore()
    await flushPromises()
    expect(p.wrapper.find('input[value="light"]').exists()).toBe(false)
    expect(useTheme().mode.value).toBe('system')
    const clean = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(clean)
    expect(clean.defaultPrevented).toBe(false)
  })
})
