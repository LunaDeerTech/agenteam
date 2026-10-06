import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter, isNavigationFailure } from 'vue-router'
import type { SessionController } from '../composables/useSession'
const selected = vi.hoisted(() => ({ auth: null as SessionController | null }))
vi.mock('../composables/useSession', async (original) => ({
  ...(await original<typeof import('../composables/useSession')>()),
  useSession: () => selected.auth!,
}))
import { createSessionController } from '../composables/useSession'
import { createAccountAPI, type SessionView } from '../api/account'
import { createSystemAccountAPI } from '../api/system-account'
import { createSystemInvitationAPI } from '../api/system-invitations'
import { createSystemProviderAPI } from '../api/system-providers'
import { createSystemModelAPI } from '../api/system-models'
import { createSystemModelSelectionAPI } from '../api/system-model-selection'
import { createSystemAccountSecurityAPI } from '../api/system-account-security'
import type { Fetch } from '../api/client'
import { installAuthentication, safeReturnTarget } from '../router/auth'
import { useTheme } from '../composables/useTheme'
import App from '../App.vue'
import HomeView from '../views/HomeView.vue'
import LoginView from '../views/auth/LoginView.vue'
import SystemSettingsView from '../views/system/SystemSettingsView.vue'
import SystemUsersView from '../views/system/SystemUsersView.vue'
import SystemAccountSecurityView from '../views/system/SystemAccountSecurityView.vue'

const id = (n: number) => '01900000-0000-7000-8000-' + n.toString(16).padStart(12, '0')
const time = '2026-10-06T12:34:56.123456Z'
const sessionView = (): SessionView => ({
  user: {
    id: id(1),
    email: 'admin@example.com',
    username: 'admin',
    display_name: 'Admin',
    role: 'admin',
    theme: 'dark',
    version: '1',
    initial_password_suggestion: false,
  },
  session: { id: id(2), issued_at: time, idle_expires_at: time, absolute_expires_at: time },
  csrf_token: 'S'.repeat(43),
})
const settings = (version = '1') => ({
  id: id(10),
  version,
  session_idle_seconds: '604800',
  session_absolute_seconds: '2592000',
  password_reset_seconds: '1800',
  challenge_after_failures: '5',
  lifetime_changes_apply_to: 'newly_issued_sessions_and_tokens',
})
const json = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), {
    status,
    headers: {
      'Content-Type': status >= 400 ? 'application/problem+json' : 'application/json',
      'X-Request-ID': id(9),
    },
  })
const problem = (code: string, status = 503) =>
  json(
    {
      type: 'urn:agenteam:problem:test',
      title: 'Failure',
      status,
      code,
      detail: '<private detail>',
      instance: '/api/v1/system/account-settings',
      request_id: id(9),
      commit_state: 'not_started',
    },
    status,
  )
function barrier<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  const promise = new Promise<T>((done) => (resolve = done))
  return { promise, resolve }
}
let wrapper: VueWrapper | undefined
beforeEach(() => {
  vi.stubGlobal(
    'matchMedia',
    vi.fn((media: string) => ({
      media,
      matches: false,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  )
  // DOM/state evidence. Native focus, pointer defaults and layout remain browser checks.
  vi.spyOn(HTMLElement.prototype, 'getClientRects').mockImplementation(
    () => [new DOMRect(0, 0, 80, 24)] as unknown as DOMRectList,
  )
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  selected.auth?.leave()
  document.body.innerHTML = ''
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  useTheme().setTheme('system')
})
async function page(
  options: { path?: string; role?: 'admin' | 'user'; anonymous?: boolean; missing?: boolean } = {},
) {
  let current = sessionView(),
    signedIn = !options.anonymous,
    observed = settings()
  current = { ...current, user: { ...current.user, role: options.role ?? 'admin' } }
  let session: Fetch = async () => (signedIn ? json(current) : problem('UNAUTHENTICATED', 401))
  let read: Fetch = async () => (options.missing ? problem('NOT_FOUND', 404) : json(observed))
  let mutation: Fetch = async (_, init) => {
    const input = JSON.parse(init.body as string)
    observed = { ...settings(), ...input, version: String(BigInt(input.version) + 1n) }
    return json(observed)
  }
  const fetch = vi.fn<Fetch>(async (path, init) => {
    if (path === '/api/v1/session') return session(path, init)
    if (path === '/api/v1/system/account-settings')
      return init.method === 'GET' ? read(path, init) : mutation(path, init)
    if (path.startsWith('/api/v1/system/users?')) return json({ items: [] })
    if (path.endsWith('/bootstrap'))
      return json({
        csrf_token: 'A'.repeat(43),
        challenge_modes: ['rotate'],
        delivery_channel: 'backend_log',
      })
    if (path.endsWith('/login')) {
      signedIn = true
      return json({ user: current.user, session: current.session, next_path: '/' })
    }
    if (path.endsWith('/logout')) {
      signedIn = false
      return new Response(null, { status: 204 })
    }
    throw new Error('Unexpected test route')
  })
  selected.auth = createSessionController(
    createAccountAPI(fetch),
    createSystemAccountAPI(fetch),
    createSystemInvitationAPI(fetch),
    createSystemProviderAPI(fetch),
    createSystemModelAPI(fetch),
    createSystemModelSelectionAPI(fetch),
    createSystemAccountSecurityAPI(fetch),
  )
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: HomeView, meta: { authentication: true, protected: true } },
      { path: '/login', name: 'login', component: LoginView, meta: { authentication: true } },
      {
        path: '/settings/profile',
        component: { template: '<h1>个人资料</h1>' },
        meta: { authentication: true, protected: true },
      },
      {
        path: '/system',
        component: SystemSettingsView,
        redirect: '/system/users',
        meta: {
          authentication: true,
          protected: true,
          systemAdmin: true,
          navigation: { label: '系统设置', order: 20 },
        },
        children: [
          { path: 'users', component: SystemUsersView },
          { path: 'account-security', component: SystemAccountSecurityView },
          ...['invitations', 'providers', 'models', 'model-selection'].map((path) => ({
            path,
            component: { template: '<p>Accepted leaf outside this interaction</p>' },
          })),
        ],
      },
    ],
  })
  installAuthentication(router, selected.auth)
  await router.push(options.path ?? '/system/account-security')
  await router.isReady()
  const host = document.createElement('div')
  host.id = 'app'
  document.body.append(host)
  wrapper = mount(App, { attachTo: host, global: { plugins: [router] } })
  await flushPromises()
  return {
    wrapper,
    router,
    auth: selected.auth,
    fetch,
    setRead(value: Fetch) {
      read = value
    },
    setMutation(value: Fetch) {
      mutation = value
    },
    setSession(value: SessionView) {
      current = value
    },
    setSessionRequest(value: Fetch) {
      session = value
    },
    resetSession() {
      session = async () => (signedIn ? json(current) : problem('UNAUTHENTICATED', 401))
    },
    writes() {
      return fetch.mock.calls.filter(
        ([path, init]) => path === '/api/v1/system/account-settings' && init.method === 'PUT',
      )
    },
  }
}
function button(label: string, root: ParentNode = document) {
  const value = [...root.querySelectorAll<HTMLButtonElement>('button')].find(
    (node) =>
      (
        node.querySelector('.button-label[aria-hidden="false"]')?.textContent ?? node.textContent
      )?.trim() === label || node.getAttribute('aria-label') === label,
  )
  expect(value, `button ${label}`).toBeTruthy()
  return value!
}
async function click(label: string, root: ParentNode = document) {
  button(label, root).click()
  await flushPromises()
}
function field(label: string) {
  const node = [...document.querySelectorAll<HTMLLabelElement>('label')].find(
    (value) => value.textContent?.replace(/\s*\*$/, '') === label,
  )
  expect(node, `field ${label}`).toBeTruthy()
  return document.getElementById(node!.htmlFor) as HTMLInputElement
}
async function change(label: string, value: string) {
  const input = field(label)
  input.value = value
  input.dispatchEvent(new Event('input', { bubbles: true }))
  await flushPromises()
}
async function save() {
  const submitter = button('保存配置')
  submitter.form!.dispatchEvent(
    new SubmitEvent('submit', { bubbles: true, cancelable: true, submitter }),
  )
  await flushPromises()
}
function confirmation() {
  const layers = [...document.querySelectorAll<HTMLElement>('[role="dialog"]')]
  expect(layers).toHaveLength(1)
  expect(layers[0]!.inert).toBe(false)
  expect(layers[0]!.getAttribute('aria-hidden')).toBeNull()
  return layers[0]!
}

describe('Account security inline form in the actual App and router', () => {
  it('keeps users as default, renders six leaves in three groups and only adds the tenth exact return', async () => {
    const f = await page({ path: '/system' })
    expect(f.router.currentRoute.value.path).toBe('/system/users')
    expect(
      f.wrapper
        .get('nav[aria-label="系统设置"]')
        .findAll('a')
        .map((a) => a.text()),
    ).toEqual(['用户', '待注册邀请', 'Providers', 'Models', '平台模型用途', '账号安全'])
    expect(f.wrapper.findAll('.settings-group-toggle').map((group) => group.text())).toEqual([
      '用户与邀请',
      '模型与提供商',
      '平台配置',
    ])
    await f.wrapper.get('a[href="/system/account-security"]').trigger('click')
    await flushPromises()
    expect(f.wrapper.get('h1').text()).toBe('账号安全')
    expect(f.wrapper.findAll('form')).toHaveLength(1)
    expect(f.wrapper.findAll('input[inputmode="numeric"]')).toHaveLength(4)
    expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(0)
    expect(button('保存配置').disabled).toBe(true)
    expect(safeReturnTarget('/system/account-security')).toBe('/system/account-security')
    for (const value of [
      '/system/account-security?x=1',
      '/system/account-security#x',
      '/system/account-security/',
      '/system/account-security/extra',
      ['/system/account-security'],
      '//evil/system/account-security',
      'https://evil/system/account-security',
    ])
      expect(safeReturnTarget(value)).toBe('/')
    const { router } = await import('../router/index')
    expect(router.resolve('/system/account-security').meta).toMatchObject({
      authentication: true,
      protected: true,
      systemAdmin: true,
    })
  })

  it('mounts no configuration form or admin GET/PUT for a regular user direct link', async () => {
    const f = await page({ role: 'user' })
    expect(f.wrapper.text()).toContain('无权访问系统设置')
    expect(f.wrapper.find('nav[aria-label="系统设置"]').exists()).toBe(false)
    expect(f.wrapper.find('form').exists()).toBe(false)
    expect(f.fetch.mock.calls.some(([path]) => path.startsWith('/api/v1/system/'))).toBe(false)
  })

  it('returns through the actual login form and does not seed defaults after an unavailable singleton', async () => {
    const f = await page({ anonymous: true, missing: true })
    expect(f.router.currentRoute.value.query.return).toBe('/system/account-security')
    await f.wrapper.get('#login-email').setValue('admin@example.com')
    await f.wrapper.get('#login-password').setValue('Original password 123')
    await f.wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(f.router.currentRoute.value.path).toBe('/system/account-security')
    expect(f.wrapper.text()).toContain('账号安全配置读取失败')
    expect(f.wrapper.find('form').exists()).toBe(false)
    expect(f.writes()).toHaveLength(0)
  })

  it('keeps string validation visible and requires a real discard decision for cancel and overwrite', async () => {
    const f = await page()
    await change('会话空闲期限（秒）', ' 900')
    expect(field('会话空闲期限（秒）').value).toBe(' 900')
    expect(field('会话空闲期限（秒）').getAttribute('aria-invalid')).toBe('true')
    await save()
    expect(f.writes()).toHaveLength(0)
    await click('取消修改')
    await click('继续编辑', confirmation())
    expect(field('会话空闲期限（秒）').value).toBe(' 900')
    await click('读取当前配置')
    await click('放弃修改', confirmation())
    expect(field('会话空闲期限（秒）').value).toBe('604800')
    expect(button('保存配置').disabled).toBe(true)
    expect(f.writes()).toHaveLength(0)
  })

  it.each(['route', 'logout'] as const)(
    'restores the one inline discard Dialog after pageshow checking/503 for pending %s',
    async (action) => {
      const f = await page()
      const originalHeading = f.wrapper.get('h1').element
      await change('密码重置有效期（秒）', '3600')
      const route = action === 'route' ? f.router.push('/system/users') : null
      if (action === 'logout') button('退出登录').click()
      await flushPromises()
      confirmation()
      const session = barrier<Response>()
      f.setSessionRequest(() => session.promise)
      window.dispatchEvent(new Event('pageshow'))
      await flushPromises()
      expect(f.wrapper.find('.session-check').exists()).toBe(true)
      expect(document.querySelectorAll('.ui-overlay')).toHaveLength(0)
      expect(document.getElementById('app')!.inert).toBe(false)
      expect(document.documentElement.style.overflow).toBe('')
      session.resolve(problem('UNAVAILABLE'))
      await flushPromises()
      expect(button('检查当前会话').disabled).toBe(false)
      expect(document.querySelectorAll('.ui-overlay')).toHaveLength(0)
      f.resetSession()
      await click('检查当前会话')
      const top = confirmation()
      const currentHeading = f.wrapper.get('h1').element
      expect(currentHeading).not.toBe(originalHeading)
      expect(originalHeading.isConnected).toBe(false)
      expect(field('密码重置有效期（秒）').value).toBe('3600')
      expect(top.contains(document.activeElement)).toBe(true)
      await click('继续编辑', top)
      expect(document.activeElement).toBe(currentHeading)
      if (route) expect(isNavigationFailure(await route)).toBe(true)
      expect(f.router.currentRoute.value.path).toBe('/system/account-security')
      expect(f.writes()).toHaveLength(0)
      expect(f.fetch.mock.calls.filter(([path]) => path.endsWith('/logout'))).toHaveLength(0)
      const next = action === 'route' ? f.router.push('/system/users') : null
      if (action === 'logout') button('退出登录').click()
      await flushPromises()
      await click('放弃修改', confirmation())
      if (next) await next
      await flushPromises()
      expect(f.router.currentRoute.value.path).toBe(action === 'route' ? '/system/users' : '/login')
      expect(document.querySelectorAll('.ui-overlay')).toHaveLength(0)
      expect(f.writes()).toHaveLength(0)
    },
  )

  it('retains all conflict fields until an actual adopt button, never using GET to rebase the next PUT silently', async () => {
    const f = await page()
    await change('密码重置有效期（秒）', '3600')
    f.setMutation(async () => problem('VERSION_CONFLICT', 409))
    await save()
    expect(f.wrapper.text()).toContain('需要核对最新配置')
    f.setRead(async () => json({ ...settings('3'), password_reset_seconds: '7200' }))
    await click('读取最新配置')
    expect(field('密码重置有效期（秒）').value).toBe('3600')
    expect(f.wrapper.get('form p code').text()).toBe('1')
    expect(f.writes()).toHaveLength(1)
    await click('采用最新值重新编辑')
    await click('放弃修改', confirmation())
    expect(field('密码重置有效期（秒）').value).toBe('7200')
    expect(f.wrapper.get('form p code').text()).toBe('3')
    expect(button('保存配置').disabled).toBe(true)
    expect(f.writes()).toHaveLength(1)
  })

  it('shows strict historical confirmation plus failed current read, then allows only GET until a fresh baseline', async () => {
    const f = await page()
    await change('触发挑战的失败次数（次）', '7')
    f.setRead(async () => problem('UNAVAILABLE'))
    await save()
    expect(f.wrapper.text()).toContain('保存已确认，当前配置读取失败')
    expect(f.wrapper.text()).toContain('原请求已确认，历史结果版本：2')
    expect(f.wrapper.find('form').exists()).toBe(false)
    expect(f.writes()).toHaveLength(1)
    f.setRead(async () => json(settings('3')))
    await click('读取当前配置')
    await change('密码重置有效期（秒）', '3600')
    expect(button('保存配置').getAttribute('aria-label')).toBeNull()
    expect(f.writes()).toHaveLength(1)
    expect(f.wrapper.get('form p code').text()).toBe('3')
  })

  it('settles a pending route false on a same-user new Session and never carries the draft or old DOM continuation', async () => {
    const f = await page()
    const originalHeading = f.wrapper.get('h1').element
    await change('密码重置有效期（秒）', '3600')
    const pending = f.router.push('/system/users')
    await flushPromises()
    confirmation()
    const next = sessionView()
    f.setSession({ ...next, session: { ...next.session, id: id(3) } })
    window.dispatchEvent(new Event('pageshow'))
    await flushPromises()
    expect(isNavigationFailure(await pending)).toBe(true)
    expect(document.querySelectorAll('.ui-overlay')).toHaveLength(0)
    expect(field('密码重置有效期（秒）').value).toBe('1800')
    expect(f.router.currentRoute.value.path).toBe('/system/account-security')
    expect(originalHeading.isConnected).toBe(false)
    expect(document.activeElement).toBe(f.wrapper.get('h1').element)
    expect(f.writes()).toHaveLength(0)
  })
})
